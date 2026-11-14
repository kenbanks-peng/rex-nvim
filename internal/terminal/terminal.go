// Package terminal implements the renderer side of the private manager protocol.
package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Message is a JSON message exchanged over the helper's private Unix socket.
// Paste keys contain literal text; input keys use nvim_input notation.
type Message struct {
	Type      string  `json:"type"`
	Title     *string `json:"title,omitempty"`
	Directory *string `json:"directory,omitempty"`
	Bell      bool    `json:"bell,omitempty"`
	Block     string  `json:"block,omitempty"`
	Grid      int     `json:"grid,omitempty"`
	Width     int     `json:"width,omitempty"`
	Height    int     `json:"height,omitempty"`
	Keys      string  `json:"keys,omitempty"`
	Button    string  `json:"button,omitempty"`
	Action    string  `json:"action,omitempty"`
	Modifier  string  `json:"modifier,omitempty"`
	Row       int     `json:"row,omitempty"`
	Col       int     `json:"col,omitempty"`
	Frame     string  `json:"frame,omitempty"`
}

const (
	enterModes  = "\x1b[?1049h\x1b[?25l\x1b[?1000l\x1b[?1003l\x1b[?1002l\x1b[?1006l\x1b[?1004h\x1b[?2004h"
	leaveModes  = "\x1b[?1000l\x1b[?1003l\x1b[?2004l\x1b[?1004l\x1b[?1006l\x1b[?1002l\x1b[0m\x1b]112\x1b\\\x1b[0 q\x1b[?25h\x1b[?1049l"
	escapeDelay = 40 * time.Millisecond
)

// Run connects a renderer to socket and runs until cancellation or disconnection.
// SIGTSTP suspends only this renderer, retaining its backend connection.
// The caller owns termination signals, including SIGTERM and SIGHUP.
func Run(ctx context.Context, socket string, grid int) (err error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return fmt.Errorf("connect renderer: %w", err)
	}
	defer conn.Close()

	inputFD := int(os.Stdin.Fd())
	state, err := term.MakeRaw(inputFD)
	if err != nil {
		return fmt.Errorf("raw terminal: %w", err)
	}
	restore := func() error {
		// Restore termios even if writing the terminal reset sequence fails.
		_, writeErr := io.WriteString(os.Stdout, leaveModes)
		return errors.Join(writeErr, term.Restore(inputFD, state))
	}
	defer func() { err = errors.Join(err, restore()) }()
	if _, err = io.WriteString(os.Stdout, enterModes); err != nil {
		return err
	}

	resizes := make(chan os.Signal, 1)
	signal.Notify(resizes, syscall.SIGWINCH)
	defer signal.Stop(resizes)
	size := func() (int, int, error) {
		return term.GetSize(inputFD)
	}
	suspends := make(chan os.Signal, 1)
	signal.Notify(suspends, syscall.SIGTSTP)
	defer signal.Stop(suspends)
	suspend := func() error {
		if err := restore(); err != nil {
			return err
		}
		// Stop this process, never its process group or the detached backend.
		// SIGSTOP works even when Rex launched us in an orphaned job group.
		if err := syscall.Kill(os.Getpid(), syscall.SIGSTOP); err != nil {
			return err
		}
		if _, err := term.MakeRaw(inputFD); err != nil {
			return err
		}
		_, err := io.WriteString(os.Stdout, enterModes)
		return err
	}
	return runSessionLifecycle(ctx, conn, inputFD, os.Stdout, grid, size, resizes, suspends, suspend)
}

type inputEvent struct {
	message Message
	err     error
}

func runSession(ctx context.Context, conn net.Conn, inputFD int, output io.Writer, grid int,
	size func() (int, int, error), resizes <-chan os.Signal,
) error {
	return runSessionLifecycle(ctx, conn, inputFD, output, grid, size, resizes, nil, nil)
}

func runSessionLifecycle(ctx context.Context, conn net.Conn, inputFD int, output io.Writer, grid int,
	size func() (int, int, error), resizes, suspends <-chan os.Signal, suspend func() error,
) error {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	// Closing the socket also interrupts a blocked JSON Encode or Decode.
	stopClose := context.AfterFunc(ctx, func() { conn.Close() })
	defer func() {
		cancel()
		conn.Close()
		stopClose()
		workers.Wait()
	}()
	result := func(err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	width, height, err := size()
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(Message{Type: "hello", Block: os.Getenv("REX_BLOCK"), Grid: grid, Width: width, Height: height}); err != nil {
		return result(err)
	}

	input := make(chan inputEvent, 32)
	frames := make(chan inputEvent, 1)
	var inputCancel context.CancelFunc
	var inputDone chan struct{}
	startInput := func() {
		var inputCtx context.Context
		inputCtx, inputCancel = context.WithCancel(ctx)
		inputDone = make(chan struct{})
		go func() {
			defer close(inputDone)
			readInput(inputCtx, inputFD, input)
		}()
	}
	stopInput := func() { inputCancel(); <-inputDone }
	startInput()
	defer stopInput()
	workers.Add(1)
	go func() {
		defer workers.Done()
		decoder := json.NewDecoder(conn)
		for {
			var m Message
			err := decoder.Decode(&m)
			select {
			case frames <- inputEvent{message: m, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	lastFrame := ""
	haveFrame := false
	paint := func() error {
		_, err := io.WriteString(output, "\x1b[H"+lastFrame)
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, ok := <-suspends:
			if !ok {
				suspends = nil
				continue
			}
			stopInput()
			if err := encoder.Encode(Message{Type: "suspend", Grid: grid}); err != nil {
				return result(err)
			}
			// The writer acknowledges only after disabling frame/host delivery.
			// Keep draining until then so its write deadline cannot expire.
		waiting:
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case event := <-frames:
					if event.err != nil {
						return result(event.err)
					}
					switch event.message.Type {
					case "suspended":
						break waiting
					case "frame":
						lastFrame, haveFrame = event.message.Frame, true
					case "host":
						if _, err := io.WriteString(output, HostControls(event.message)); err != nil {
							return err
						}
					}
				}
			}
			if err := suspend(); err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := encoder.Encode(Message{Type: "resume", Grid: grid}); err != nil {
				return result(err)
			}
			startInput()
			width, height, err := size()
			if err != nil {
				return err
			}
			if err := encoder.Encode(Message{Type: "resize", Grid: grid, Width: width, Height: height}); err != nil {
				return result(err)
			}
			if haveFrame {
				if err := paint(); err != nil {
					return err
				}
			}
		case event := <-frames:
			if event.err != nil {
				return result(event.err)
			}
			if event.message.Type == "host" {
				if _, err := io.WriteString(output, HostControls(event.message)); err != nil {
					return err
				}
			}
			if event.message.Type == "frame" {
				lastFrame, haveFrame = event.message.Frame, true
				if err := paint(); err != nil {
					return err
				}
			}
		case event := <-input:
			if event.err != nil {
				return result(event.err)
			}
			m := event.message
			m.Grid = grid
			if err := encoder.Encode(m); err != nil {
				return result(err)
			}
		case _, ok := <-resizes:
			if !ok {
				resizes = nil
				continue
			}
			width, height, err := size()
			if err != nil {
				return err
			}
			if err := encoder.Encode(Message{Type: "resize", Grid: grid, Width: width, Height: height}); err != nil {
				return result(err)
			}
			if haveFrame {
				if err := paint(); err != nil {
					return err
				}
			}
		}
	}
}

// Polling avoids a permanently blocked stdin goroutine without closing the
// caller's stdin or changing its shared file status flags.
func readInput(ctx context.Context, fd int, events chan<- inputEvent) {
	send := func(event inputEvent) bool {
		select {
		case events <- event:
			return true
		case <-ctx.Done():
			return false
		}
	}
	parser := inputParser{}
	var escapeAt time.Time
	buf := make([]byte, 4096)
	for {
		if ctx.Err() != nil {
			return
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(poll, 10)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			send(inputEvent{err: err})
			return
		}
		var messages []Message
		if n > 0 {
			if poll[0].Revents&unix.POLLNVAL != 0 {
				send(inputEvent{err: unix.EBADF})
				return
			}
			n, err := unix.Read(fd, buf)
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
				continue
			}
			if err != nil {
				send(inputEvent{err: err})
				return
			}
			if n == 0 {
				for _, m := range parser.expire() {
					if !send(inputEvent{message: m}) {
						return
					}
				}
				send(inputEvent{err: io.EOF})
				return
			}
			messages = parser.feed(buf[:n])
			escapeAt = time.Now()
		} else if parser.waitingEscape() && time.Since(escapeAt) >= escapeDelay {
			messages = parser.expire()
			escapeAt = time.Now()
		}
		for _, m := range messages {
			if !send(inputEvent{message: m}) {
				return
			}
		}
	}
}
