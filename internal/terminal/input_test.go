package terminal

import (
	"reflect"
	"strings"
	"testing"
)

func inputs(keys ...string) []Message {
	out := make([]Message, len(keys))
	for i, k := range keys {
		out[i] = Message{Type: "input", Keys: k}
	}
	return out
}

func TestInputParser(t *testing.T) {
	tests := []struct {
		name string
		data string
		want []Message
	}{
		{"text", "a<é界🙂", inputs("a", "<LT>", "é", "界", "🙂")},
		{"controls", "\x00\x01\x03\t\n\r\x08\x1c\x1d\x1e\x1f\x7f", inputs("<C-@>", "<C-A>", "<C-C>", "<Tab>", "<C-J>", "<CR>", "<C-H>", "<C-\\>", "<C-]>", "<C-^>", "<C-_>", "<BS>")},
		{"arrows", "\x1b[A\x1b[B\x1b[C\x1b[D", inputs("<Up>", "<Down>", "<Right>", "<Left>")},
		{"navigation", "\x1b[H\x1b[F\x1b[1~\x1b[4~\x1b[2~\x1b[3~\x1b[5~\x1b[6~", inputs("<Home>", "<End>", "<Home>", "<End>", "<Insert>", "<Del>", "<PageUp>", "<PageDown>")},
		{"functions", "\x1bOP\x1bOQ\x1bOR\x1bOS\x1b[15~\x1b[24~\x1b[34~", inputs("<F1>", "<F2>", "<F3>", "<F4>", "<F5>", "<F12>", "<F20>")},
		{"modifiers", "\x1b[1;2A\x1b[1;5D\x1b[3;3~\x1b[1;8P\x1b[Z", inputs("<S-Up>", "<C-Left>", "<A-Del>", "<S-C-A-F1>", "<S-Tab>")},
		{"alt", "\x1bx\x1b<\x1bé\x1b\x03", inputs("<A-x>", "<A-LT>", "<A-é>", "<A-C-C>")},
		{"extended keys", "\x1b[97;5u\x1b[27;3;60~", inputs("<C-a>", "<A-LT>")},
		{"focus", "\x1b[I\x1b[O", []Message{{Type: "focus"}, {Type: "blur"}}},
		{"paste", pasteStart + "a<\r\n🙂\x1b[A" + pasteEnd + "x", []Message{{Type: "paste", Keys: "a<\r\n🙂\x1b[A"}, {Type: "input", Keys: "x"}}},
		{"empty paste", pasteStart + pasteEnd, []Message{{Type: "paste"}}},
		{"consecutive paste", pasteStart + "a" + pasteEnd + pasteStart + "b" + pasteEnd, []Message{{Type: "paste", Keys: "a"}, {Type: "paste", Keys: "b"}}},
		{"unknown sequence", "\x1b[99~\x1b[?25h\x1b[1;99Ax", inputs("x")},
		{"invalid numeric fields", "\x1b[?1A\x1b[27;3;~\x1b[4294967393ux", inputs("x")},
		{"extra numeric fields", "\x1b[1;2;3Ax", inputs("x")},
		{"alt controls", "\x1b\t\x1b\r\x1b\x7f\x1b\x00", inputs("<A-Tab>", "<A-CR>", "<A-BS>", "<A-C-@>")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Every possible two-read split, plus one-byte-at-a-time delivery.
			for split := 0; split <= len(tt.data); split++ {
				var p inputParser
				got := p.feed([]byte(tt.data[:split]))
				got = append(got, p.feed([]byte(tt.data[split:]))...)
				if !reflect.DeepEqual(got, tt.want) {
					t.Fatalf("split %d: got %#v, want %#v", split, got, tt.want)
				}
			}
			var p inputParser
			var got []Message
			for i := range len(tt.data) {
				got = append(got, p.feed([]byte{tt.data[i]})...)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("bytewise: got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestEscapeExpiry(t *testing.T) {
	var p inputParser
	if got := p.feed([]byte{27}); len(got) != 0 {
		t.Fatal(got)
	}
	if got := p.expire(); !reflect.DeepEqual(got, inputs("<Esc>")) {
		t.Fatal(got)
	}
	p.feed([]byte("\x1b["))
	if got := p.expire(); !reflect.DeepEqual(got, inputs("<Esc>", "[")) {
		t.Fatal(got)
	}
	p.feed([]byte{0xe7})
	if got := p.expire(); len(got) != 0 {
		t.Fatal("partial UTF-8 expired", got)
	}
	if got := p.feed([]byte{0x95, 0x8c}); !reflect.DeepEqual(got, inputs("界")) {
		t.Fatal(got)
	}
	p.feed([]byte(pasteStart + "hello"))
	if got := p.expire(); len(got) != 0 {
		t.Fatal("paste expired", got)
	}
	if got := p.feed([]byte(pasteEnd)); !reflect.DeepEqual(got, []Message{{Type: "paste", Keys: "hello"}}) {
		t.Fatal(got)
	}
}

func TestLargePaste(t *testing.T) {
	var p inputParser
	p.feed([]byte(pasteStart))
	content := strings.Repeat("ab<界\n", 10000)
	for i := 0; i < len(content); i += 13 {
		end := min(i+13, len(content))
		if got := p.feed([]byte(content[i:end])); len(got) != 0 {
			t.Fatal("premature paste", got)
		}
		if len(p.buf) >= len(pasteEnd) {
			t.Fatal("paste parser retains excessive lookbehind")
		}
	}
	got := p.feed([]byte(pasteEnd))
	if !reflect.DeepEqual(got, []Message{{Type: "paste", Keys: content}}) {
		t.Fatal("paste content mismatch")
	}
}

func TestMouse(t *testing.T) {
	data := "\x1b[<0;1;1M\x1b[<32;5;3M\x1b[<0;5;3m" +
		"\x1b[<1;2;4M\x1b[<1;2;4m\x1b[<2;8;9M\x1b[<3;8;9M" +
		"\x1b[<64;1;1M\x1b[<65;1;1M\x1b[<66;1;1M\x1b[<67;1;1M" +
		"\x1b[<28;2;2M\x1b[<35;3;4M"
	want := []Message{
		{Type: "mouse", Button: "left", Action: "press"},
		{Type: "mouse", Button: "left", Action: "drag", Row: 2, Col: 4},
		{Type: "mouse", Button: "left", Action: "release", Row: 2, Col: 4},
		{Type: "mouse", Button: "middle", Action: "press", Row: 3, Col: 1},
		{Type: "mouse", Button: "middle", Action: "release", Row: 3, Col: 1},
		{Type: "mouse", Button: "right", Action: "press", Row: 8, Col: 7},
		{Type: "mouse", Button: "right", Action: "release", Row: 8, Col: 7},
		{Type: "mouse", Button: "wheel", Action: "up"},
		{Type: "mouse", Button: "wheel", Action: "down"},
		{Type: "mouse", Button: "wheel", Action: "left"},
		{Type: "mouse", Button: "wheel", Action: "right"},
		{Type: "mouse", Button: "left", Action: "press", Modifier: "S-C-A-", Row: 1, Col: 1},
		{Type: "mouse", Button: "move", Row: 3, Col: 2},
	}
	for split := 0; split <= len(data); split++ {
		var p inputParser
		got := p.feed([]byte(data[:split]))
		got = append(got, p.feed([]byte(data[split:]))...)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("split %d: got %#v", split, got)
		}
	}
	for _, bad := range []string{"\x1b[<0;0;1M", "\x1b[<;1;1M", "\x1b[<128;1;1M"} {
		var p inputParser
		if got := p.feed([]byte(bad)); len(got) != 0 {
			t.Fatalf("invalid mouse %q: %#v", bad, got)
		}
	}
}
