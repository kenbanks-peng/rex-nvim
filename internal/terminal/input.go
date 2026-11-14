package terminal

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"
)

// inputParser retains partial terminal sequences between reads.
type inputParser struct {
	buf        []byte
	pasting    bool
	paste      bytes.Buffer
	lastButton string
}

const pasteStart = "\x1b[200~"
const pasteEnd = "\x1b[201~"

func (p *inputParser) feed(data []byte) []Message {
	p.buf = append(p.buf, data...)
	var out []Message
	for len(p.buf) > 0 {
		if p.pasting {
			if i := bytes.Index(p.buf, []byte(pasteEnd)); i >= 0 {
				p.paste.Write(p.buf[:i])
				out = append(out, Message{Type: "paste", Keys: p.paste.String()})
				p.paste.Reset()
				p.pasting = false
				p.buf = p.buf[i+len(pasteEnd):]
				continue
			}
			// Keep a possible fragmented closing marker, not the whole paste.
			n := len(p.buf) - len(pasteEnd) + 1
			if n > 0 {
				p.paste.Write(p.buf[:n])
				p.buf = p.buf[n:]
			}
			break
		}
		if bytes.HasPrefix(p.buf, []byte(pasteStart)) {
			p.pasting = true
			p.buf = p.buf[len(pasteStart):]
			continue
		}
		m, n := p.next()
		if n == 0 {
			break
		}
		p.buf = p.buf[n:]
		if m.Type != "" {
			out = append(out, m)
		}
	}
	return out
}

func (p *inputParser) waitingEscape() bool {
	return !p.pasting && len(p.buf) > 0 && p.buf[0] == 27
}

// expire disambiguates Escape from an unfinished escape sequence. UTF-8 and
// bracketed pastes never expire.
func (p *inputParser) expire() []Message {
	if !p.waitingEscape() {
		return nil
	}
	p.buf = p.buf[1:]
	out := []Message{{Type: "input", Keys: "<Esc>"}}
	return append(out, p.feed(nil)...)
}

func key(name, modifiers string) Message {
	return Message{Type: "input", Keys: "<" + modifiers + name + ">"}
}

func literal(r rune) string {
	switch r {
	case '<':
		return "<LT>"
	case 0:
		return "<C-@>"
	case 9:
		return "<Tab>"
	case 10:
		return "<C-J>"
	case 13:
		return "<CR>"
	case 27:
		return "<Esc>"
	case 127:
		return "<BS>"
	}
	if r < 32 {
		return "<C-" + string(r+'@') + ">"
	}
	return string(r)
}

func (p *inputParser) next() (Message, int) {
	b := p.buf
	if b[0] != 27 {
		if !utf8.FullRune(b) {
			return Message{}, 0
		}
		r, n := utf8.DecodeRune(b)
		return Message{Type: "input", Keys: literal(r)}, n
	}
	if len(b) == 1 {
		return Message{}, 0
	}
	if b[1] != '[' && b[1] != 'O' {
		if !utf8.FullRune(b[1:]) {
			return Message{}, 0
		}
		r, n := utf8.DecodeRune(b[1:])
		s := literal(r)
		if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") {
			return Message{Type: "input", Keys: "<A-" + s[1:]}, n + 1
		}
		return key(s, "A-"), n + 1
	}
	for i := 2; i < len(b); i++ {
		if b[i] >= 0x40 && b[i] <= 0x7e {
			body, final := string(b[2:i]), b[i]
			if b[1] == '[' && strings.HasPrefix(body, "<") && (final == 'M' || final == 'm') {
				return p.mouse(body[1:], final), i + 1
			}
			return sequence(body, final), i + 1
		}
		if i >= 128 || b[i] < 0x20 {
			return key("Esc", ""), 1
		}
	}
	return Message{}, 0
}

func modifiers(code int) string {
	if code < 1 || code > 16 {
		return ""
	}
	bits := code - 1
	var s string
	if bits&1 != 0 {
		s += "S-"
	}
	if bits&4 != 0 {
		s += "C-"
	}
	if bits&2 != 0 {
		s += "A-"
	}
	if bits&8 != 0 {
		s += "D-"
	}
	return s
}

func sequence(body string, final byte) Message {
	if body == "" {
		switch final {
		case 'I':
			return Message{Type: "focus"}
		case 'O':
			return Message{Type: "blur"}
		case 'Z':
			return key("Tab", "S-")
		}
	}
	parts := strings.Split(body, ";")
	n, err := strconv.Atoi(parts[0])
	if body != "" && (err != nil || n < 0) {
		return Message{}
	}
	if len(parts) > 2 && !(final == '~' && n == 27 && len(parts) == 3) {
		return Message{}
	}
	mod := ""
	if len(parts) > 1 {
		code, err := strconv.Atoi(parts[1])
		if err != nil || code < 1 || code > 16 {
			return Message{}
		}
		mod = modifiers(code)
	}
	names := map[byte]string{
		'A': "Up", 'B': "Down", 'C': "Right", 'D': "Left",
		'H': "Home", 'F': "End", 'P': "F1", 'Q': "F2", 'R': "F3", 'S': "F4",
	}
	if name := names[final]; name != "" {
		return key(name, mod)
	}
	if final == '~' {
		name := map[int]string{
			1: "Home", 2: "Insert", 3: "Del", 4: "End", 5: "PageUp", 6: "PageDown",
			7: "Home", 8: "End", 11: "F1", 12: "F2", 13: "F3", 14: "F4",
			15: "F5", 17: "F6", 18: "F7", 19: "F8", 20: "F9", 21: "F10",
			23: "F11", 24: "F12", 25: "F13", 26: "F14", 28: "F15",
			29: "F16", 31: "F17", 32: "F18", 33: "F19", 34: "F20",
		}[n]
		if name != "" {
			return key(name, mod)
		}
		// xterm modifyOtherKeys: CSI 27 ; modifier ; codepoint ~
		if n == 27 && len(parts) == 3 {
			n, err = strconv.Atoi(parts[2])
			if err != nil {
				return Message{}
			}
			return modifiedRune(n, mod)
		}
	}
	if final == 'u' {
		return modifiedRune(n, mod)
	}
	return Message{}
}

func modifiedRune(n int, mod string) Message {
	if n < 0 || n > utf8.MaxRune || !utf8.ValidRune(rune(n)) {
		return Message{}
	}
	s := literal(rune(n))
	if mod == "" {
		return Message{Type: "input", Keys: s}
	}
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") {
		s = s[1 : len(s)-1]
	}
	return key(s, mod)
}

func (p *inputParser) mouse(body string, final byte) Message {
	parts := strings.Split(body, ";")
	if len(parts) != 3 {
		return Message{}
	}
	values := [3]int{}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return Message{}
		}
		values[i] = n
	}
	b, col, row := values[0], values[1]-1, values[2]-1
	if b > 127 || col < 0 || row < 0 {
		return Message{}
	}
	m := Message{Type: "mouse", Row: row, Col: col}
	if b&4 != 0 {
		m.Modifier += "S-"
	}
	if b&16 != 0 {
		m.Modifier += "C-"
	}
	if b&8 != 0 {
		m.Modifier += "A-"
	}
	if b&64 != 0 {
		m.Button = "wheel"
		m.Action = []string{"up", "down", "left", "right"}[b&3]
		return m
	}
	if b&32 != 0 && b&3 == 3 {
		m.Button = "move"
		return m
	}
	m.Button = []string{"left", "middle", "right", ""}[b&3]
	switch {
	case final == 'm' || b&3 == 3 && b&32 == 0:
		m.Action = "release"
		if m.Button == "" {
			m.Button = p.lastButton
		}
		p.lastButton = ""
	case b&32 != 0:
		m.Action = "drag"
	default:
		m.Action = "press"
		p.lastButton = m.Button
	}
	if m.Button == "" {
		return Message{}
	}
	return m
}
