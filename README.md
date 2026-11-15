# rex-nvim

A Go frontend that runs one headless Neovim and renders its windows in separate
[Rex](https://www.superlogical.com/rex) blocks. Buffers, registers, undo, plugins,
and language servers belong to that single editor process.

## Install

Requires macOS or Linux, Go 1.26+, Neovim 0.11+, and Rex with API revision 13+.
Both `nvim` and `rex` must be on your shell's `PATH`.

```sh
make install
```

This builds `~/.local/bin/rex-nvim`. Add that directory to `PATH`, or set
`PREFIX` when installing elsewhere.

## Use

Run inside a Rex terminal:

```sh
rex-nvim file.go
rex-nvim --clean file.go
```

Neovim arguments pass through unchanged. Your normal configuration loads;
`vim.g.rex_nvim` is set before initialization for frontend-specific configuration.

- `:split` and `:vsplit` create Rex blocks. Neovim window moves and resize commands
  update the Rex layout.
- Rex focus and tiled terminal-size changes update Neovim. Closing a Rex block
  closes its Neovim window; a rejected modified-buffer close recreates the block.
- All Neovim floating windows use Rex floating blocks, including non-focusable
  overlays. They can span the full Rex window canvas, independently of tiled
  blocks. Neovim controls their size and position; smaller terminals clip the
  rendered content without resizing plugin windows. Non-focusable floats leave
  keyboard focus in the editor.
- Shared UI uses the full Rex window canvas: one global statusline
  (`laststatus=3`), native tabline, command line, messages and completion menus
  render in floating layers, never repeated inside editing splits. Window-local
  winbars also use floating layers. Neovim
  controls formatting and mouse behavior; custom statusline/tabline plugins work
  through their native grids.
- Neovim tab pages switch the editing splits shown in the existing Rex tab.
  `showtabline` controls the native, full-width top tabline.
- `:qa` exits the shared editor. Closing its **last tiled Rex block terminates
  the editor, including unsaved changes**. Closing the original block does not
  terminate other editor blocks.

A detached backend owns Neovim; terminal helpers exchange input and rendered
frames over a private local socket. No Neovim plugin installation is required.

## Current limits

Experimental. Floating-pane placement is approximate because Rex's headers and
padding are not Neovim cells. Neovim tabs are not separate Rex tabs. Rex-side
pane rearrangements do not rearrange Neovim windows; use Neovim window commands.
OS clipboard integration depends on Neovim's configured clipboard provider.
Terminal suspension and terminal-specific graphics protocols are not supported.
See the [UI capability policy](docs/ui-capabilities.md) for protocol coverage and
unsupported-event diagnostics.

## Development

```sh
make build
make test
go vet ./...
python3 scripts/smoke.py
python3 scripts/window_ui.py
```

Unit tests include real-Neovim redraw checks when `nvim` is available. The smoke
test requires a running Rex server, creates a disposable session, and removes
it afterward. It does not switch the app's displayed session.
