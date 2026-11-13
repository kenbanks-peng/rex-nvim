package terminal

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

func inputWorker(t *testing.T) (*os.File, <-chan inputEvent) {
	t.Helper()
	stdin, keyboard, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan inputEvent, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		readInput(ctx, int(stdin.Fd()), events)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("input worker did not stop")
		}
		stdin.Close()
		keyboard.Close()
	})
	return keyboard, events
}

func expectInput(t *testing.T, events <-chan inputEvent, want Message) {
	t.Helper()
	select {
	case event := <-events:
		if event.err != nil || !reflect.DeepEqual(event.message, want) {
			t.Fatalf("got %#v / %v, want %#v", event.message, event.err, want)
		}
	case <-time.After(time.Second):
		t.Fatal("input timed out")
	}
}

func TestInputTimeouts(t *testing.T) {
	t.Run("escape", func(t *testing.T) {
		keyboard, events := inputWorker(t)
		keyboard.WriteString("\x1b")
		expectInput(t, events, Message{Type: "input", Keys: "<Esc>"})
		keyboard.WriteString("x")
		expectInput(t, events, Message{Type: "input", Keys: "x"})
	})
	t.Run("incomplete CSI", func(t *testing.T) {
		keyboard, events := inputWorker(t)
		keyboard.WriteString("\x1b[")
		expectInput(t, events, Message{Type: "input", Keys: "<Esc>"})
		expectInput(t, events, Message{Type: "input", Keys: "["})
	})
	t.Run("unknown CSI", func(t *testing.T) {
		keyboard, events := inputWorker(t)
		keyboard.WriteString("\x1b[999~\x1b[?25h")
		time.Sleep(2 * escapeDelay)
		keyboard.WriteString("x")
		expectInput(t, events, Message{Type: "input", Keys: "x"})
	})
	t.Run("fragmented UTF8 does not expire", func(t *testing.T) {
		keyboard, events := inputWorker(t)
		keyboard.Write([]byte{0xe7})
		time.Sleep(2 * escapeDelay)
		keyboard.Write([]byte{0x95, 0x8c})
		expectInput(t, events, Message{Type: "input", Keys: "界"})
	})
	t.Run("paste does not expire", func(t *testing.T) {
		keyboard, events := inputWorker(t)
		keyboard.WriteString(pasteStart + "raw<\x1b[A\r\n")
		time.Sleep(2 * escapeDelay)
		keyboard.WriteString("界" + pasteEnd[:3])
		time.Sleep(2 * escapeDelay)
		keyboard.WriteString(pasteEnd[3:] + "x")
		expectInput(t, events, Message{Type: "paste", Keys: "raw<\x1b[A\r\n界"})
		expectInput(t, events, Message{Type: "input", Keys: "x"})
	})
}

func TestFocusWireFormat(t *testing.T) {
	var parser inputParser
	events := parser.feed([]byte("\x1b[I\x1b[O"))
	if len(events) != 1 {
		t.Fatalf("focus/blur generated %#v", events)
	}
	events[0].Grid = 7
	data, err := json.Marshal(events[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\"type\":\"focus\",\"grid\":7}" {
		t.Fatalf("unexpected wire format: %s", data)
	}
}
