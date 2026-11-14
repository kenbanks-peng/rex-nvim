#!/usr/bin/env python3
"""Completion integration checks in isolated Rex sessions."""
import json
import os
import tempfile
from pathlib import Path
from smoke import run, wait


def check(binary, tmp, grid):
    socket = str(Path(tmp) / ("grid.sock" if grid else "external.sock"))
    created = json.loads(run(
        "rex", "new", "rex-popupmenu-check", "--json", "--shell=none", "--",
        "/usr/bin/env", f"PATH={os.environ['PATH']}",
        f"REX_NVIM_POPUPMENU={'grid' if grid else 'external'}",
        binary, "--clean", "-n", "--listen", socket))
    session = created["session_id"]
    initial = created["initial_windows"][0]["block_ids"][0]

    def lua(code):
        return run("nvim", "--server", socket, "--remote-expr",
                   "luaeval(" + json.dumps(code) + ")").strip()

    def blocks():
        data = json.loads(run("rex", "block", "ls", "-s", session, "--json"))
        return [b["block_id"] for b in data["blocks"]
                if not b.get("label", "").startswith("rex-statusline:")]

    def capture(block):
        return run("rex", "capture", "-s", session, "-b", block, "--trim")

    def write(block, data):
        run("rex", "block", "call", "-s", session, "-b", block, "write",
            json.dumps({"data": data}))

    def complete():
        lua("(function() vim.cmd('startinsert!');return true end)()")
        wait(lambda: lua("vim.api.nvim_get_mode().mode").startswith("i"), "insert mode")
        lua("(function() local items={};for i=1,40 do "
            "items[i]=string.format('COMPLETE%02d',i) end;"
            "vim.fn.complete(1,items);return true end)()")

    def menu_block():
        return next((b for b in blocks() if "COMPLETE01" in capture(b)), None)

    try:
        wait(lambda: lua("vim.fn.getpid()"), "startup")
        lua("(function() vim.o.mouse='a';vim.o.completeopt='menuone,noselect';"
            "vim.o.hidden=true;vim.cmd('vsplit');vim.cmd('enew');return true end)()")
        wait(lambda: len(blocks()) == 2, "two tiled renderers")
        lua("(function() vim.api.nvim_buf_set_lines(0,0,-1,false,{'TILE_BODY'});return true end)()")
        owner = wait(lambda: next((b for b in blocks() if "TILE_BODY" in capture(b)), None), "owner tile")
        complete()
        wait(menu_block, "tiled completion")
        if not grid:
            assert len(blocks()) == 2, "external menu created a stray grid renderer"
            wait(lambda: lua("vim.fn.pum_getpos().height") not in ("0", ""),
                 "geometry feedback")
        write(owner, "\x0e" * 30)
        wait(lambda: any("COMPLETE30" in capture(b) for b in blocks()), "selection scrolls")
        run("rex", "block", "call", "-s", session, "-b", owner, "resize",
            json.dumps({"columns": 32, "rows": 12}))
        wait(lambda: any("COMPLETE30" in capture(b) for b in blocks()), "menu survives resize")
        write(owner, "\x05\x1b")
        wait(lambda: lua("vim.fn.pumvisible()") == "0", "cancellation")
        wait(lambda: all("COMPLETE30" not in capture(b) for b in blocks()), "cancel repaint")
        lua("(function() local a=vim.api; Float=a.nvim_open_win("
            "a.nvim_create_buf(false,true),true,{relative='editor',row=2,col=4,"
            "width=28,height=9});return true end)()")
        wait(lambda: len(blocks()) == 3, "float renderer")
        lua("(function() vim.api.nvim_buf_set_lines(0,0,-1,false,{'FLOAT_BODY'});return true end)()")
        floating = wait(lambda: next((b for b in blocks() if "FLOAT_BODY" in capture(b)), None), "float body")
        complete()
        wait(menu_block, "float completion")
        assert floating != owner, "menu leaked to previous tile"
        write(floating, "\x0e\x19")
        wait(lambda: lua("vim.fn.pumvisible()") == "0", "accept completion")
        assert lua("vim.api.nvim_get_current_line()") == "COMPLETE01"
        complete()
        wait(menu_block, "second float menu")
        # Close only the disposable test's float pane while completion is live.
        run("rex", "block", "close", "-s", session, floating)
        wait(lambda: lua("tostring(vim.api.nvim_win_is_valid(Float))") == "false",
             "pane loss closes float")
        wait(lambda: lua("vim.fn.pumvisible()") == "0", "pane loss clears completion")
        write(owner, "\x1b")
        wait(lambda: lua("vim.api.nvim_get_mode().mode") == "n", "normal after pane loss")
        complete()
        wait(menu_block, "completion after pane loss")
        print(f"PASS: {'grid fallback' if grid else 'external'} completion: tiles, floats, scrolling, resize, cancel, accept, pane loss")
    finally:
        run("rex", "kill", session)


def main():
    with tempfile.TemporaryDirectory(prefix="rex-popupmenu-", dir="/tmp") as tmp:
        binary = str(Path(tmp) / "rex-nvim")
        run("go", "build", "-o", binary, "./cmd/rex-nvim")
        check(binary, tmp, False)
        check(binary, tmp, True)


if __name__ == "__main__":
    main()
