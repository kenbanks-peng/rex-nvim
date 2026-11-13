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
- Rex focus and terminal-size changes update Neovim. Closing a Rex block closes
  its Neovim window; a rejected modified-buffer close recreates the block.
- Focusable Neovim floats use Rex floating blocks. Nonfocusable floats and
  completion menus stay composited with their owning pane.
- Neovim tab pages switch the windows shown in the existing Rex tab.
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

## Development

```sh
make build
make test
go vet ./...
python3 scripts/smoke.py
```

Unit tests include real-Neovim redraw checks when `nvim` is available. The smoke
test requires a running Rex server, creates a disposable session, and removes
it afterward. It does not switch the app's displayed session.
