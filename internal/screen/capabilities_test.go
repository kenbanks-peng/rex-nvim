package screen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/neovim/go-client/nvim"
)

func inventoryNames(t *testing.T, methods bool) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for _, c := range Capabilities() {
		if c.Family == "" || c.Since == "" || c.Fallback == "" || c.Rationale == "" {
			t.Fatalf("incomplete capability: %+v", c)
		}
		switch c.Status {
		case "supported", "partial", "optional", "unsupported":
		default:
			t.Fatalf("invalid status: %+v", c)
		}
		list := c.Events
		if methods {
			list = c.Methods
		}
		for _, name := range strings.Fields(list) {
			if names[name] {
				t.Fatalf("duplicate inventory entry %s", name)
			}
			names[name] = true
			if !methods && c.Status == "supported" && !handledEvents[name] {
				t.Errorf("supported event has no handler: %s", name)
			}
		}
	}
	return names
}

func TestCapabilityInventoryAndDispatcher(t *testing.T) {
	names := inventoryNames(t, false)
	inventoryNames(t, true)
	f, err := parser.ParseFile(token.NewFileSet(), "state.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string]bool{"flush": true}
	// Inspect only State.apply's top-level switch (not nested cursor shapes).
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "apply" {
			continue
		}
		for _, stmt := range fn.Body.List {
			sw, ok := stmt.(*ast.SwitchStmt)
			if !ok {
				continue
			}
			for _, item := range sw.Body.List {
				for _, expr := range item.(*ast.CaseClause).List {
					name, err := strconv.Unquote(expr.(*ast.BasicLit).Value)
					if err != nil {
						t.Fatal(err)
					}
					actual[name] = true
				}
			}
		}
	}
	if !reflect.DeepEqual(actual, handledEvents) {
		t.Fatalf("dispatch allowlist differs from handlers: actual=%v allowlist=%v", actual, handledEvents)
	}
	for name := range actual {
		if !names[name] {
			t.Errorf("unclassified handler %s", name)
		}
	}
	// Critical omissions must stay explicit rather than silently disappear.
	for _, name := range strings.Fields("hl_group_set update_menu connect restart ui_send put option_set") {
		if _, ok := eventCapability(name); !ok {
			t.Errorf("missing policy for %s", name)
		}
	}
	want := map[string]any{"rgb": true, "ext_linegrid": true, "ext_multigrid": true}
	got := UIOptions()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unsafe extension allowlist: %v", got)
	}
	got["ext_messages"] = true
	if !reflect.DeepEqual(UIOptions(), want) {
		t.Fatal("attachment options share mutable storage")
	}
}

func TestNeovimCapabilityMetadata(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim unavailable")
	}
	nv, err := nvim.NewChildProcess(nvim.ChildProcessArgs("--embed", "--headless", "--clean", "-n"))
	if err != nil {
		t.Fatal(err)
	}
	defer nv.Close()
	var metadata map[string][]string
	if err := nv.ExecLua(`
local m = vim.fn.api_info()
local out = {events={},methods={}}
for _, e in ipairs(m.ui_events) do table.insert(out.events,e.name) end
for _, f in ipairs(m.functions) do
  if f.name:match('^nvim_ui_') then table.insert(out.methods,f.name) end
end
return out
`, &metadata); err != nil {
		t.Fatal(err)
	}
	if len(metadata["events"]) < 50 || len(metadata["methods"]) < 8 {
		t.Fatalf("missing metadata: %+v", metadata)
	}
	for _, list := range []struct {
		actual []string
		known  map[string]bool
	}{
		{metadata["events"], inventoryNames(t, false)},
		{metadata["methods"], inventoryNames(t, true)},
	} {
		for _, name := range list.actual {
			if !list.known[name] {
				t.Errorf("unclassified Neovim protocol member %s", name)
			}
		}
	}
}

func TestIgnoredEventDiagnosticsAreBoundedAndDoNotRenderPayloads(t *testing.T) {
	s, baseline := New(), New()
	setup := [][]any{event("grid_resize", 1, 8, 2), event("grid_line", 1, 0, 0, []any{[]any{"SAFE"}}, false)}
	s.Apply(setup...)
	baseline.Apply(setup...)
	var reports []string
	suppressed := 0
	s.OnIgnoredEvent = func(name string, limit bool) {
		if limit {
			suppressed++
			return
		}
		reports = append(reports, name)
	}
	payload := "\x1b]52;c;UNSAFE\a"
	for range 1000 {
		s.Apply(event("ui_send", payload), event("future_event", payload))
	}
	if len(reports) != 2 || suppressed != 0 {
		t.Fatalf("duplicate diagnostics: %v, %d", reports, suppressed)
	}
	// Include no-argument events, nested batches, and future appended fields.
	if !s.Apply([]any{event("connect", payload), event("future_no_args"), event("flush")}) {
		t.Fatal("unknown events lost flush")
	}
	for i := 0; i < 1000; i++ {
		s.Apply(event(fmt.Sprintf("future_%d", i), payload))
	}
	for range 1000 {
		s.Apply([]any{strings.Repeat("x", 10000), []any{payload}})
	}
	if len(reports) != 32 || suppressed != 1 || len(s.ignoredEvents) != 32 {
		t.Fatalf("unbounded diagnostics: reports=%d suppressed=%d storage=%d", len(reports), suppressed, len(s.ignoredEvents))
	}
	for _, name := range reports {
		if len(name) > 128 || strings.Contains(name, payload) {
			t.Fatalf("unsafe diagnostic %q", name)
		}
	}
	if s.Render(1, 8, 2) != baseline.Render(1, 8, 2) {
		t.Fatal("ignored events changed grid rendering")
	}
	s.Apply(event("grid_line", 1, 0, 0, []any{[]any{"OK"}}, false, "future parameter"))
	baseline.Apply(event("grid_line", 1, 0, 0, []any{[]any{"OK"}}, false))
	if s.Render(1, 8, 2) != baseline.Render(1, 8, 2) {
		t.Fatal("future parameter broke rendering")
	}
}

func TestIgnoredEventNameLength(t *testing.T) {
	s := New()
	var got string
	s.OnIgnoredEvent = func(name string, _ bool) { got = name }
	s.Apply([]any{strings.Repeat("x", 10000)})
	if len(got) != 128 {
		t.Fatalf("name length %d", len(got))
	}
}
