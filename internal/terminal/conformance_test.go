package terminal

import (
	"reflect"
	"testing"
)

// A renderer stream interleaves OS focus, command editing, popup navigation,
// history paging and bracketed paste; controls inside paste are never events.
func TestProtocolInputStreamFragmentation(t *testing.T) {
	stream := "[I:echo '界é🙂'[D[O" +
		pasteStart + "界[I[<0;2;3M<CR>" + pasteEnd +
		"[I[<0;2;3M[6~"
	want := []Message{{Type: "focus"}}
	want = append(want, inputs(":", "e", "c", "h", "o", " ", "'", "界", "e", "́", "🙂", "'", "<Left>", "<BS>", "<CR>")...)
	want = append(want, Message{Type: "blur"},
		Message{Type: "paste", Keys: "界[I[<0;2;3M<CR>"},
		Message{Type: "focus"}, Message{Type: "mouse", Button: "left", Action: "press", Row: 2, Col: 1})
	want = append(want, inputs("<PageDown>", "<C-C>")...)
	for chunk := 1; chunk <= len(stream); chunk++ {
		var parser inputParser
		var got []Message
		for start := 0; start < len(stream); start += chunk {
			got = append(got, parser.feed([]byte(stream[start:min(start+chunk, len(stream))]))...)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("chunk %d: got %#v, want %#v", chunk, got, want)
		}
		if tail := parser.expire(); len(tail) != 0 {
			t.Fatalf("chunk %d retained input: %#v", chunk, tail)
		}
	}
}
