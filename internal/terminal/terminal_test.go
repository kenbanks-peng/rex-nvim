package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"reflect"
	"syscall"
	"testing"
	"time"
)

type frameWriter chan string

func (w frameWriter) Write(p []byte) (int, error) {
	w <- string(p)
	return len(p), nil
}

func TestSession(t *testing.T) {
	t.Setenv("REX_BLOCK", "")
	helper, manager := net.Pipe()
	defer manager.Close()
	if err := manager.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	stdin, keyboard, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	defer keyboard.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frames := make(frameWriter, 4)
	resizes := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() {
		done <- runSession(ctx, helper, int(stdin.Fd()), frames, 7,
			func() (int, int, error) { return 80, 24, nil }, resizes)
	}()
	decoder, encoder := json.NewDecoder(manager), json.NewEncoder(manager)
	read := func(want Message) {
		t.Helper()
		var got Message
		if err := decoder.Decode(&got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
	read(Message{Type: "hello", Grid: 7, Width: 80, Height: 24})
	if err := encoder.Encode(Message{Type: "frame", Frame: "screen"}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-frames:
		if got != "\x1b[Hscreen" {
			t.Fatal(got)
		}
	case <-time.After(time.Second):
		t.Fatal("frame not rendered")
	}
	if _, err := keyboard.WriteString("\x1b[A" + pasteStart + "<raw>\n" + pasteEnd + "\x1b[I"); err != nil {
		t.Fatal(err)
	}
	read(Message{Type: "input", Grid: 7, Keys: "<Up>"})
	read(Message{Type: "paste", Grid: 7, Keys: "<raw>\n"})
	read(Message{Type: "focus", Grid: 7})
	if _, err := keyboard.Write([]byte{27}); err != nil {
		t.Fatal(err)
	}
	read(Message{Type: "input", Grid: 7, Keys: "<Esc>"})
	resizes <- syscall.SIGWINCH
	read(Message{Type: "resize", Grid: 7, Width: 80, Height: 24})
	select {
	case <-frames:
	case <-time.After(time.Second):
		t.Fatal("resize did not redraw")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("session did not cancel with stdin idle")
	}
}

func TestCancellationDuringHello(t *testing.T) {
	helper, manager := net.Pipe()
	defer manager.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runSession(ctx, helper, -1, io.Discard, 1,
			func() (int, int, error) { return 80, 24, nil }, nil)
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked hello did not cancel")
	}
}

func TestReadInputCancellation(t *testing.T) {
	for _, data := range []string{"", "\x1b", "\xe7", pasteStart + "unfinished"} {
		t.Run(data, func(t *testing.T) {
			stdin, keyboard, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			defer keyboard.Close()
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				defer close(done)
				readInput(ctx, int(stdin.Fd()), make(chan inputEvent))
			}()
			keyboard.WriteString(data)
			time.Sleep(15 * time.Millisecond)
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("input worker leaked")
			}
		})
	}
}

// This subprocess is attached to a real PTY by TestRunRestoresTTY.
func TestRunProcess(t *testing.T) {
	socket := os.Getenv("REX_TERMINAL_TEST_SOCKET")
	if socket == "" {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	err := Run(ctx, socket, 9)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRunRestoresTTY(t *testing.T) {
	t.Setenv("REX_BLOCK", "")
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 required for PTY integration test")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", ptyTest, binary, enterModes, leaveModes)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("PTY lifecycle: %v\n%s", err, out)
	}
}

const ptyTest = `
import fcntl, json, os, pty, signal, socket, struct, subprocess, sys, tempfile, termios

for shutdown in ("cancel", "disconnect", "bad-json"):
    with tempfile.TemporaryDirectory(prefix="rex-tty-", dir="/tmp") as directory:
        path = directory + "/s"
        listener = socket.socket(socket.AF_UNIX)
        listener.bind(path)
        listener.listen(1)
        listener.settimeout(5)
        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
        before = termios.tcgetattr(slave)
        env = dict(os.environ, REX_TERMINAL_TEST_SOCKET=path)
        proc = subprocess.Popen([sys.argv[1], "-test.run=^TestRunProcess$"], stdin=slave, stdout=slave,
                                stderr=subprocess.PIPE, env=env)
        conn = None
        try:
            conn, _ = listener.accept()
            conn.settimeout(5)
            reader = conn.makefile("rb")
            hello = json.loads(reader.readline())
            assert hello == {"type":"hello", "grid":9, "width":80, "height":24}, hello
            assert not termios.tcgetattr(slave)[3] & termios.ICANON
            conn.sendall(b'{"type":"frame","frame":"FRAME"}\n')
            os.write(master, b"\x1b[A")
            assert json.loads(reader.readline()) == {"type":"input", "grid":9, "keys":"<Up>"}
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
            proc.send_signal(signal.SIGWINCH)
            assert json.loads(reader.readline()) == {"type":"resize", "grid":9, "width":100, "height":30}
            if shutdown == "cancel":
                os.write(master, b"\x1b[200~unfinished")
                proc.send_signal(signal.SIGTERM)
            elif shutdown == "disconnect":
                conn.shutdown(socket.SHUT_RDWR)
            else:
                conn.sendall(b'not-json\n')
            code = proc.wait(timeout=5)
            after = termios.tcgetattr(slave)
            # Darwin sets PENDIN when returning to canonical input.
            after[3] &= ~termios.PENDIN
            before[3] &= ~termios.PENDIN
            assert after == before, ("termios not restored", before, after)
            os.set_blocking(master, False)
            output = b""
            while True:
                try:
                    chunk = os.read(master, 65536)
                    if not chunk: break
                    output += chunk
                except BlockingIOError:
                    break
            assert sys.argv[2].encode() in output, repr(output)
            assert sys.argv[3].encode() in output, repr(output)
            assert b"FRAME" in output, repr(output)
            assert (code == 0) == (shutdown != "bad-json"), (code, proc.stderr.read())
        finally:
            if proc.poll() is None:
                proc.kill()
                proc.wait()
            if conn: conn.close()
            listener.close()
            os.close(master)
            os.close(slave)
`
