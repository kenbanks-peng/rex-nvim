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
- `python3 scripts/smoke.py`: disposable Rex checks of splits, input, floats,
  tab restoration and cleanup.
- `python3 scripts/window_ui.py`: full-width native statusline/tabline/command
  line, floating winbars, stable fitting and chrome-renderer recovery. Neither
  switches the displayed session or touches unrelated panes.
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

## Attachment and presentation

The backend enables `rgb`, `ext_linegrid` and `ext_multigrid`. Neovim
renders tablines, completion menus, command lines and messages natively;
external widget extensions are not requested. The former `REX_NVIM_*`
presentation switches no longer select backend behavior.

Normal editing splits use tiled Rex blocks. Native floats, the root tabline,
the global statusline/command area and the native message grid use independent
Rex floating layers across the full window canvas. `laststatus=3` is enforced
so there is exactly one statusline. Custom Neovim formatting remains native.
The message layer follows `msg_set_pos`, including expanded blocking prompts.

Only editing content is rendered inside tiles; shared chrome is not copied
into each split. The first row of editing panes reserves any native tabline.
Mouse cells in chrome map back to root-grid coordinates; editing-pane input
accounts for that reservation. Keyboard input from chrome continues in the
current Neovim window. Closing a chrome renderer recreates it without closing
a Neovim window.

Full-window dimensions are measured from the tiled layout and its normalized
Rex footprint. Shared chrome is reserved once, not per split. Native layout
fitting pauses during command entry and blocking prompts, while message-layer
presentation remains live. Smaller float terminals clip their native grids;
they do not resize plugin windows.

Input uses `nvim_input`, `nvim_paste` and `nvim_input_mouse`. Rex pane focus
selects the corresponding Neovim window independently of editor-wide
`nvim_ui_set_focus`. Terminal focus losses coalesce for 75ms; unchanged
snapshots cannot infer OS activation. Root resize uses `nvim_ui_try_resize`;
genuine external windows use `nvim_ui_try_resize_grid`.

Dynamic UI-protocol toggles are not exposed. `ext_hlstate`, `ext_termcolors`,
legacy wildmenu and terminal-specific extensions remain disabled.
`mousemoveevent` controls host motion reporting; fonts, spacing and pointer
styling use host defaults. Legacy cell drawing is not implemented.

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
| Message zindex / compindex | Native messages use a full-width Rex layer ordered with other floats. |

Separate Rex floating layers follow Neovim's float order using focus-preserving
relative restacks, only when their observed order differs. Only rex-nvim-owned
layers are moved. Native message layers use their message zindex alongside
plugin floats. Host placement
is not pixel-identical to a single Neovim canvas.

Run `REX_NVIM_HOST_TEST=1 go test ./internal/screen -run TestRexRenderingFidelity -count=1`
to round-trip generated styles through a disposable Rex terminal and query its
applied cursor color. Run `python3 scripts/fidelity.py` for real Neovim/Rex
creation-time and dynamic float-order checks with focus preservation. Both use
disposable sessions without changing the displayed session. Embedded Neovim
and synthetic protocol regressions run in the normal Go suite.
Cursor color and shape are restored when the renderer exits.

## Native widgets

Neovim tab pages switch the editing splits in the current Rex tab; they never
create or rename Rex tabs. The native top tabline follows `showtabline` and
preserves custom formatting, Unicode and Neovim's mouse selection. There are
no per-pane tab/buffer footer widgets.

Completion menus use Neovim's floating grids and can cross tiled split borders.
Neovim owns selection, scrolling, acceptance, cancellation, labels and sizing.
The backend does not synthesize completion documentation or report external
popup geometry.

Colon/search commands, command blocks, nested prompts, echo/errors, mode text
and message history retain Neovim's native grids, highlighting, cursor and
pagination. They are presented in full-width floating regions, not repeated in
the invoking split. Native prompts own all input; sibling-pane input reaches
the same command editor without a window-selection RPC. Unicode rendering
uses the cells and continuation markers supplied by Neovim.

The screen package retains independently tested external-widget renderers for
protocol conformance, but the backend does not attach those extensions.
The older `tabline.py`, `popupmenu.py`, `cmdline.py` and `messages.py` scripts
describe the former pane-local presentation and are not current backend smoke
checks. Use `scripts/window_ui.py` for shared native chrome.

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

Only top viewport margins affect presentation:

- `win_viewport`: buffer ranges and `scroll_delta` are for scrollbars/smooth
  animation. Rex has neither. Apply `grid_scroll`/`grid_line` directly: they
  already describe the new viewport, even when metadata arrives later.
  Mouse wheels use grid-aware `nvim_input_mouse`, not buffer-line arithmetic.
- `win_viewport_margins`: top margins identify tiled winbar rows, which render
  in independent floating layers instead of tiled blocks. Native grid rows
  remain unchanged for cursor and mouse input. Other margins stay in their
  native grids; external windows and plugin floats retain all decorations.
- `win_extmark`: no Rex-owned overlay needs buffer-to-screen coordinates.
  Neovim renders signs, highlights, virtual text/lines and supplies updated
  `win_float_pos` for buffer-relative floats. rex-nvim never creates
  `ui_watched` marks (this is an extmark property, not a UI extension).
  Plugin-requested notifications are safely ignored.

Winbar margins belong to their grid and disappear on grid destruction.
Viewport ranges and extmark positions are not retained. Synthetic
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
