package terminal

import (
	"net/url"
	"strings"
	"testing"
)

func TestHostControlsAreSafe(t *testing.T) {
	title := "title ' ; $(touch /tmp/nope)\n\r\x00\x07\x1b]52;c;attack\x1b\\\u009c"
	path := "/tmp/quotes ';$(touch nope)/\n\x07\x1b\\?foo#bar%/日本"
	got := HostControls(Message{Title: &title, Directory: &path, Bell: true})
	wantTitle := "\x1b]2;title ' ; $(touch /tmp/nope)]52;c;attack\\\x1b\\"
	if !strings.HasPrefix(got, wantTitle) {
		t.Fatalf("unsafe title: %q", got)
	}
	directory := strings.TrimSuffix(strings.TrimPrefix(got, wantTitle+"\x1b]7;"), "\x1b\\\a")
	u, err := url.Parse(directory)
	if err != nil || u.Scheme != "file" || u.Host != "localhost" || u.Path != path {
		t.Fatalf("path changed or unescaped: %q, %v", directory, err)
	}
	if strings.ContainsAny(directory, "\x00\x07\x1b\n\r") {
		t.Fatal("raw controls in OSC 7")
	}
	for _, bad := range []string{"relative", "", "/bad\x00path"} {
		if HostControls(Message{Directory: &bad}) != "" {
			t.Fatalf("unsafe directory accepted: %q", bad)
		}
	}
	empty := ""
	if HostControls(Message{Title: &empty}) != "\x1b]2;\x1b\\" {
		t.Fatal("cannot clear title")
	}
}
