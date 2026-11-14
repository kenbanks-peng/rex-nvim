# Neovim UI capability policy

Requires Neovim 0.11+ (API level 13). The inventory covers the 0.11/0.12
protocol, including API level 14 additions; newer events remain safe to ignore.
API levels in the inventory are protocol introduction levels, not minimum
rex-nvim versions. Level 12 is Neovim 0.10, 13 is 0.11, and 14 is 0.12.

The authoritative, tested family inventory is
[`screen.Capabilities`](../internal/screen/capabilities.go). Each family lists
events/outgoing UI methods, introduction constraints, support status, fallback
and rationale. **Supported** means handled; **partial** describes specific
limitations; **optional** means deliberately disabled, not implemented;
**unsupported** means intentionally ignored/not invoked. A method's presence
does not mean it is called; see each family’s policy.
Tests compare all UI events and `nvim_ui_*` methods with embedded Neovim metadata,
and check inventory/dispatcher agreement.

## Regression checks

- `go test ./... -count=1`: embedded-Neovim behavior, all 16 presentation
  combinations (including message prerequisites), actual negotiated options,
  Unicode command → message → completion transitions, focus/resize, tab
  restoration and detach; synthetic optional-field/callback/flush regressions;
  fragmented terminal focus, paste, mouse and widget-navigation input.
- `python3 scripts/smoke.py --protocol`: serial disposable Rex sessions for
  every host integration below, external and grid widget fallbacks, terminal
  fidelity/mouse modes, normal exit and child-failure cleanup. Fails on a failed
  check; never switches the displayed session or touches unrelated panes.
- Run both commands with each supported Neovim release line's `bin` first in
  `PATH` (0.11/API 13 and 0.12/API 14). Tests use the resolved `nvim`, not a
  bundled server. Embedded tests skip explicitly if it is unavailable; Rex
  tests in the ordinary Go suite require `REX_NVIM_HOST_TEST=1`. The protocol
  host suite enables them and requires a running Rex server.

Disposable Neovim children use `-n` (no swap files), so repeated/crash tests do
not exhaust the user’s unnamed-buffer swap slots. No user swap files are removed.

Host checks use terminal RPC resize, not graphical dragging or OS activation.
Manual graphical/OS focus checks remain separate; do not infer them from a
passing headless/disposable-session run. Optional fields added in API 14 are
also exercised synthetically so API 13 runs do not leave that behavior untested.

## Attachment and fallbacks

`rgb`, `ext_linegrid` and `ext_multigrid` are always enabled. The backend
also requests `ext_tabline` for its pane-local navigation;
`REX_NVIM_TABLINE=grid` disables that presentation before attachment.
The backend also requests `ext_popupmenu` for pane-local completion;
`REX_NVIM_POPUPMENU=grid` retains the Neovim grid presentation.
The backend requests `ext_cmdline` only with its complete command-line renderer;
`REX_NVIM_CMDLINE=grid` retains the native grid presentation.
The backend requests `ext_messages` only with external command lines;
`REX_NVIM_MESSAGES=grid` retains native message grids. Choosing
`REX_NVIM_CMDLINE=grid` also disables external messages because the protocol
implicitly enables external command lines. Legacy wildmenu remains
Neovim-rendered. Other external extensions remain disabled. `ext_hlstate` is off: `hl_group_set` and highlight
provenance are unnecessary for RGB cells. Legacy cell drawing is intentionally
not implemented; disabling linegrid is not supported.

Dynamic `nvim_ui_set_option` is not exposed or invoked. `option_set` maps
`mousemoveevent` to host motion reporting. Widths, colors and widgets already
arrive in grids; fonts, spacing and pointer appearance use host defaults. This is a single-owned-child UI, not a
general multi-UI capability renegotiation client. Do not enable `override`,
`ext_termcolors`, or terminal-specific options speculatively.

Input goes through `nvim_input`, `nvim_paste` and grid-aware
`nvim_input_mouse`; Rex pane focus selects the corresponding Neovim window,
separately from `nvim_ui_set_focus` (editor-wide gained/lost focus). Terminal
focus reports and Rex active-pane transitions drive that RPC, coalescing pane
losses for 75ms to avoid spurious FocusLost/FocusGained on sibling switches.
Unchanged snapshots do not override terminal application-focus loss; OS focus
requires host terminal reports. Unfocused editors do not reclaim Rex focus.
Mouse reporting follows Neovim mouse_on/off in every pane; busy_start/stop
hides/restores the cursor without blocking input. Root resize uses
`nvim_ui_try_resize`; Neovim layout owns tiled grids. Genuine external windows
use `nvim_ui_try_resize_grid` for their renderer dimensions.

## Rendering fidelity

Rex's terminal supports RGB SGR, all five underline styles (single, double,
curl, dotted, dashed), special underline colors, bold, italic, reverse,
strikethrough, faint, blinking text, conceal and overline. Missing colors use
live `default_colors_set` values; -1 restores terminal defaults. RGB and cterm
maps are independent: absent RGB keys **must not** inherit cterm overrides.
No 256-color negotiation is needed for the required Rex terminal.

| Protocol attribute | Deliberate terminal mapping |
| --- | --- |
| Cursor shape / cell_percentage | Native block, underline or bar; terminal-defined thickness, percentage ignored. |
| blinkwait/on/off | Native blinking only if both on/off are positive; otherwise steady. Exact durations and initial delay are terminal-defined. |
| Cursor attr_id | OSC 12 fill color from effective highlight background; ID 0 uses the cursor cell's effective foreground (reverse). Missing colors/IDs reset with OSC 112. |
| Cursor foreground / attr_id_lm | Host chooses cursor glyph foreground; language-map alternate ID is not selected (no reliable redraw state identifies it). |
| blend (including 100) | Opaque cells/cursor, consistently in composed grids and independent Rex float panes. No cross-pane background is available for blending. |
| altfont / URL | Normal font / plain text; no font or hyperlink controls forwarded. |
| grid_line wrap | Cells render at explicit row positions, without terminal reflow or host soft-wrap selection metadata. |
| Float compindex / screen_row/col | Use exact composition order and Neovim-resolved coordinates when present; older events use zindex/grid ID and anchors. |
| msg_set_pos scrolled / sep_char | Preserve/clamp message rows; omit the optional separator rather than invent a MsgSeparator style with ext_hlstate disabled. |
| Message zindex / compindex | Order with floats in pane-local grid composition; messages remain visible only in the active tiled pane. |

Separate Rex floating layers follow Neovim's float order using focus-preserving
relative restacks, only when their observed order differs. Only rex-nvim-owned
layers are moved. Grid fallback composition also orders messages with floats;
pane-local messages cannot interleave independent Rex layers. Host placement
is not pixel-identical to a single Neovim canvas.

Run `REX_NVIM_HOST_TEST=1 go test ./internal/screen -run TestRexRenderingFidelity -count=1`
to round-trip generated styles through a disposable Rex terminal and query its
applied cursor color. Run `python3 scripts/fidelity.py` for real Neovim/Rex
creation-time and dynamic float-order checks with focus preservation. Both use
disposable sessions without changing the displayed session. Embedded Neovim
and synthetic protocol regressions run in the normal Go suite.
Cursor color and shape are restored when the renderer exits.

## Neovim tab and buffer navigation

Each tiled pane has separate **Nvim tabs** and **Buffers** footer rows. They
never create, rename or close Rex tabs. The active entry appears first; click
entries or the left/right arrows to select previous/next handles, including
overflow. Names are ASCII-escaped and clipped to terminal cells. Mouse follows
Neovim's mouse policy. Buffer selection targets the clicked pane's Neovim
window; tab selection restores that tab's own window layout. Non-forced
selection errors (including modified buffers) leave Neovim authoritative.

Native commands/mappings own creation, naming and closing (`:tabnew`,
`:tabclose`, `:file`, `:bdelete`, etc.); protocol updates replace both lists,
including their independent active handles. All normal keyboard navigation
remains available. `showtabline=0/1/2` hides, auto-shows for multiple Neovim
tabs, or always shows both rows. Floats/external windows have no navigation
rows; panes shorter than four cells omit the widget to preserve content.

Layout subtracts footer rows from each renderer's available content height,
and reserves no Neovim root tabline row with `ext_tabline`. In grid mode,
Neovim's original tabline cells occupy one footer row and clicks target root
grid row zero; layout retains the root tabline allocation. Grid mode preserves
custom tabline formatting and Unicode, but does not add an external buffer
list. It is the fallback for hosts/presentations that cannot use the external
widget. No new native Rex widget API or client remote-control permission is
required.

Run `python3 scripts/tabline.py` for disposable Rex sessions checking both
presentations, real SGR mouse selection, buffer/tab separation, rename updates,
`showtabline` changes and active-tab split restoration. Embedded tests cover
create/select/rename/delete/close synchronization, closed handles, independent
lists and measurement without duplicated rows.

## Completion popup menu

External completion overlays render only in the anchor grid's owning pane,
including independent floats and composed descendants of external windows.
Root-grid anchors resolve to the containing tile. The menu fits above/below
the anchor, clips to terminal content (not navigation rows), and scrolls to
keep the selected item visible. Hide, close and destroy discard stale menus;
resize recomputes geometry. No additional Rex pane or extension is needed.

Keyboard completion remains Neovim-owned, including page navigation,
acceptance and cancellation. Clicking a row accepts it with
`nvim_select_popupmenu_item`; wheel up/down sends completion previous/next,
without scrolling the underlying buffer. Other menu mouse events are consumed.
Menu rows show word, kind and source with reverse-video selection; labels are
ASCII-escaped for safe, exact terminal cell widths (including Unicode).
The optional item info is retained but no documentation panel is synthesized.
With external command lines, `grid=-1` menus anchor to the owning pane’s
command text using the protocol byte offset, converted to terminal cells.

After painting, the backend reports actual visible item count with
`nvim_ui_pum_set_height` and the clipped root-cell bounding box with
`nvim_ui_pum_set_bounds`, outside the redraw lock. This gives completion
plugins the real available height and popup-relative float anchor. Independent
Rex placement is not a single pixel-identical Neovim canvas, as with other
floats. Disabling external completion before attachment preserves Neovim's
working multigrid rendering and normal grid mouse routing.

Run `python3 scripts/popupmenu.py` for isolated Rex checks of both modes:
tiled/float completion, scrolling, resize, cancellation, acceptance and pane
loss/reuse. Embedded Neovim tests verify geometry feedback, mouse selection
and real show/select/hide events; synthetic tests verify anchor isolation,
clipping, escaped labels and grid-ID reuse.

## Command lines and command blocks

Colon/search commands, input() prompts and recursive expression prompts render
in the invoking pane, above navigation rows. Each nesting level retains its own
highlighted chunks, prefix, prompt, indent, byte-indexed cursor and pending
special character. Hide/abort discards only that level; Neovim owns editing,
execution, cancellation and restoration. Completed block lines appear above
the live command and disappear on block hide.

Unicode graphemes (including combining sequences split across chunks), wide
characters and emoji use terminal-cell widths. Long commands wrap; clipped
blocks/commands follow the active cursor. Prompt highlight IDs from newer
servers are honored; older servers omit that optional field. Controls cannot
inject terminal escapes. As with terminal grids, font/ambiguous-width policy
must agree with the host; no independent Unicode font shaping is provided.

The widget overlays content only while active, suppressing duplicate grid
command/message cells. It reserves no additional Neovim rows. With grid-rendered messages, user
cmdheight is retained, including zero. With external messages, Neovim sets
cmdheight to zero on attachment; layout preserves that value until the user
changes it. Window focus changes
are deferred during command entry and blocking prompts; input from sibling panes still reaches the
shared Neovim command editor. UI gained/lost focus remains independent.
In grid fallback mode native layout fitting waits until command entry ends,
so window fitting cannot clear the native prompt during renderer resize;
existing grids clip to the terminal meanwhile. Native block history remains
clipped to the fallback message rows, unlike the external block widget.

Run `python3 scripts/cmdline.py` for disposable Rex checks of both presentations,
Unicode editing, nested/input prompts, command block editing/acceptance/abort,
focus and renderer resize. External-mode checks also verify full block context
and absence of duplicate command lines. Embedded tests cover real protocol
events, highlighted input(), cursor positions, special characters, nested
cancellation, block append/hide, completion anchors, focus and resize.

## Messages, status text and history

External messages render only in the active owning pane, above navigation rows.
Highlighted chunks share the command renderer's Unicode wrapping and control
sanitization. Errors, echo, unknown message kinds and confirmation text use the
same presentation. `replace_last`, `append`/`:echon` and newer integer/string
message IDs update existing visible entries rather than displaying duplicates.
Trigger metadata is not interpreted as an action. An empty-message-only flush
clears the shared area. `msg_clear` clears ordinary messages, not the independent
showmode/recording, showcmd and ruler fields; empty status events hide those.
Ordinary messages clear on the next non-prompt keyboard input, not a timer.
Long live output shows its tail; use Neovim history for retained messages.

On API 14, `:messages` and `g<` replace a local history snapshot; the protocol's history
flag never adds a second client-side history log. Neovim owns history retention
and clearing. Both visible messages and each history snapshot retain at most
128 entries, each truncated at a UTF-8 boundary to 8192 text bytes (oldest entries
dropped). History append fields join the preceding entry. These limits apply
only to presentation, not Neovim's actual history. PageUp/PageDown or Ctrl-B/F
page the snapshot; Enter, Escape, Space and Ctrl-C dismiss it locally. Other
keys dismiss and pass through. History stays in its invoking pane across resize
and sibling input; tiny panes need two rows to show both history and its footer.
No separate Rex window, timed notification or host scrollback is synthesized.

On API 13 (Neovim 0.11), history is followed by native hit-enter: display
the history with that prompt instead of the local viewer, and forward input to
Neovim. API 13 also clears transient echo output during root resize; new output
continues normally. The API 13 `msg_history_clear` event clears retained history and paging,
not ordinary messages or status; it was removed in API 14.

Neovim owns confirmation/input prompts and any hit-enter/more prompts. Their
keys are never swallowed by the history viewer. Blocking-state checks use the
fast `nvim_get_mode` RPC: hit-enter can retain the normal `mode_change` cursor.
Sibling input answers the shared prompt without a window-selection RPC, and
native layout fitting waits for dismissal so resize cannot clear or deadlock
that prompt. If a prompt starts after the blocking check, an already in-flight
layout RPC keeps pumping frames without issuing further layout/widget RPCs;
reconciliation resumes after it completes. The grid fallback preserves Neovim's own message/history grids and
native pagination, including when command lines are also grid-rendered.

Run `python3 scripts/messages.py` for disposable Rex checks of external messages,
message-grid fallback and all-grid fallback: echo/errors, Unicode, mode/showcmd,
paged history, confirmation, native hit-enter, sibling focus and renderer resize.
Embedded tests additionally cover ruler, highlighted history, command input after
messages, progress IDs on current servers, and native history/command-line
interactions. Synthetic tests cover bounds, append/replacement, independent
clearing, safe text and extension prerequisites.

## External windows

`win_external_pos` identifies genuine `external=true` Neovim windows, separately
from ordinary multigrid windows assigned Rex terminals. Each gets an initially
centered independent Rex layer; later host placement is retained, not projected
back onto the tiled canvas. Renderer hello/resize requests use
`nvim_ui_try_resize_grid`, without changing root dimensions or tiled topology.
Ordinary plugin floats still own their cell dimensions and anchors.

Floats/popups anchored inside an external window compose in its terminal rather
than becoming layers positioned in the root canvas. Root-anchored floats do not
leak into external terminals. Hide/close removes the layer; renderer transport
loss with a live block recreates it at its observed host bounds. Intentional
host closes use Neovim’s non-forced close: a rejected modified-buffer close
recreates the external renderer. Closing the last tiled pane still ends the
owned editor; external layers are not a standalone detached-editor service.
External windows themselves require no additional UI extension.

Run `python3 scripts/external.py` for disposable-session lifecycle, focus,
terminal resize, popup isolation, recovery and protected-close checks. Without
a graphical client, the check issues the terminal resize RPC explicitly after
changing fractional layer bounds; it does not test graphical drag gestures.
Embedded tests also verify close/hide/grid reuse and unchanged tiled dimensions.

## Viewport, margins and extmarks

These metadata events deliberately have no retained state or host consumer:

- `win_viewport`: buffer ranges and `scroll_delta` are for scrollbars/smooth
  animation. Rex has neither. Apply `grid_scroll`/`grid_line` directly: they
  already describe the new viewport, even when metadata arrives later.
  Mouse wheels use grid-aware `nvim_input_mouse`, not buffer-line arithmetic.
- `win_viewport_margins`: winbars and float borders are already inside grids.
  Render all cells and use grid coordinates unchanged for input and placement;
  subtracting margins would crop or double-offset decorations.
- `win_extmark`: no Rex-owned overlay needs buffer-to-screen coordinates.
  Neovim renders signs, highlights, virtual text/lines and supplies updated
  `win_float_pos` for buffer-relative floats. rex-nvim never creates
  `ui_watched` marks (this is an extmark property, not a UI extension).
  Plugin-requested notifications are safely ignored.

No duplicate window/grid metadata cache means hide/close/reused grid IDs cannot
inherit stale viewport ranges, margin offsets or extmark positions. Synthetic
lifecycle tests cover scrolling, resize, hide/show, close/destroy and reuse;
`TestNeovimViewportGridFallback` verifies real winbar/number columns, virtual
text/lines, bordered buffer-relative float movement, resize, tab hide/restore,
close and incremental/full-redraw agreement. It also observes actual viewport,
margin and plugin-requested extmark events without enabling external widgets.

## Host metadata and attention

`set_title` sets OSC 2 in every rex-nvim-owned pane (including new/reconnected
renderers). C0/C1 controls are removed; no text is passed to a shell.
`chdir` reports the shared editor's current directory via OSC 7 in those panes:
absolute paths are URI-encoded; relative and NUL-containing paths are ignored.
This is editor metadata, **not** a process cwd change or a per-window `lcd`
promise. Rex's `pwd` method exposes reports; `process` exposes actual cwd.
Session/tab labels and unrelated panes are never renamed or updated.

Rex has no pane icon setter: `set_icon` is retained in UI state without a host
side effect. It does not replace the title. `bell` and `visual_bell` both emit
BEL only in the Neovim cursor's pane (command-line/message cursors use their
last window). Missing panes fall back to the active, last-focused, then primary
owned renderer; without a renderer attention is discarded. Rex controls whether
BEL is audible or visual; there is no separate visual-flash API. Pending bells
coalesce per pane, so a slow renderer cannot accumulate an attention storm.

Host-only redraw batches wake delivery even without grid changes or `flush`.
Host messages are independent of replaceable/cached frames: resize repaints
never replay bells. Metadata coalesces to its latest value under backpressure.
Run `python3 scripts/host_events.py` for disposable-session checks of hostile
metadata, unchanged process cwd, multi-pane attention, and late renderers.

## UI and terminal lifecycle

The backend owns one child Neovim and one UI attachment, not one per Rex pane.
Renderer replacement/reattachment preserves buffers, modified state, windows
and the shared UI. A lost renderer socket with an existing Rex block replaces
that renderer; an intentionally removed block closes its Neovim window without
forcing modified buffers. Closing the last tiled pane ends the backend.

Normal backend shutdown first attempts `nvim_ui_detach` (250ms bound), then
cancels/closes the child RPC and all renderer sockets and removes owned
secondary panes/layers. The original/primary pane is left for its caller shell.
Child failure or a blocked detach uses RPC-close cleanup; detach is unnecessary
once the connection is dead. This does not expose a public detached-editor
service or reconnect to a different Neovim.

External **SIGTSTP** to a renderer pauses only that process. It first stops
stdin polling and obtains a backend writer acknowledgement so no frame/host
write can time out while stopped. It resets mouse/focus/paste modes, cursor
style/color/visibility, SGR and alternate screen, restores termios, then stops
itself (not its process group). **SIGCONT** resumes the same connection, raw
mode, sizing and full cached/current frames, including cursor/mouse state.
The backend and siblings remain live; no Rex pane is created or closed.
Terminal size changes while stopped are reported on resume. A backend failure
while stopped is observed on continuation; terminal cleanup still runs.
SIGTERM, SIGINT and SIGHUP exit through the same restoration path.
SIGKILL/direct SIGSTOP cannot be intercepted and cannot promise cleanup.

Neovim's `suspend` event (`:suspend`/Ctrl-Z) is deliberately declined and
diagnosed: there is no shared foreground job to suspend, nor a shell-resume
mechanism for independently spawned sibling/float renderers. It must not stop
the detached backend, all Rex processes, or silently replace the editor.
Use targeted SIGTSTP/SIGCONT for renderer suspension. No uncontrolled process
handoff is attempted.

PTY lifecycle tests verify shutdown, transport failure, actual stop/continue,
termios restoration and retained input/connection. Embedded tests verify bounded
detach and detach/reattach preservation; `python3 scripts/lifecycle.py` checks
real Rex pane recovery, sibling progress, suspension, normal exit and child
failure in disposable sessions.

## Unsupported host behavior

- `update_menu`: no native menu; mappings remain usable through Neovim.
- `connect` / `restart` (0.12+): diagnosed, no address is dialed and no
  child/transport is replaced. Neovim may detach the UI or exit; start a new
  rex-nvim instead. Transparent reconnect is not promised.
- `ui_send` / `nvim_ui_send`: no raw host-terminal forwarding.
  `stdout_tty` is not advertised. `nvim_ui_term_event` is not sent: terminal
  responses must not be routed ambiguously among shared panes.
- No shared-editor suspension via Neovim, graphics passthrough, screenshot service, native
  menu or special `error_exit` code propagation.

## Diagnostics and forward compatibility

Unhandled events (known omissions and future unknown names) leave grid state
unchanged and never abort a redraw. The backend writes a quoted, bounded event
**name only**, never arguments, to its private `backend.log` beside `ui.sock`
(`/tmp/rex-nvim-*/backend.log`, removed at backend shutdown).

Each State logs at most 32 distinct names once each, then one suppression
notice. Names and deduplication storage are bounded (128 bytes, 32 entries);
even a stream of distinct future names cannot flood logs or grow memory.
Repeated events are silent. Existing event parameters may grow; extra fields
remain ignored. Suppression lasts for the backend lifetime, not a timed window.
