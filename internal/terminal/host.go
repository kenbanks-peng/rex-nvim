package terminal

import (
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
)

// HostControls emits only the allowlisted controls, never raw protocol text.
// OSC 2 sets a pane title; OSC 7 reports editor cwd, not process cwd.
func HostControls(m Message) string {
	var out strings.Builder
	if m.Title != nil {
		title := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, *m.Title)
		out.WriteString("\x1b]2;" + title + "\x1b\\")
	}
	if m.Directory != nil && filepath.IsAbs(*m.Directory) && !strings.ContainsRune(*m.Directory, 0) {
		u := url.URL{Scheme: "file", Host: "localhost", Path: *m.Directory}
		out.WriteString("\x1b]7;" + u.String() + "\x1b\\")
	}
	if m.Bell {
		out.WriteByte(7)
	}
	return out.String()
}
