#!/usr/bin/env python3
"""Check tabline navigation and active-tab topology in disposable Rex sessions."""

import json
import os
import tempfile
from pathlib import Path

from smoke import run, wait


def check(binary, tmp, grid):
    socket = str(Path(tmp) / ("grid.sock" if grid else "external.sock"))
    created = json.loads(
        run(
            "rex", "new", "rex-tabline-check", "--json", "--shell=none", "--",
            "/usr/bin/env", f"PATH={os.environ['PATH']}",
            f"REX_NVIM_TABLINE={'grid' if grid else 'external'}",
            binary, "--clean", "-n", "--listen", socket,
        )
    )
    session = created["session_id"]
    initial = created["initial_windows"][0]["block_ids"][0]

    def lua(code):
        return run("nvim", "--server", socket, "--remote-expr",
                   "luaeval(" + json.dumps(code) + ")").strip()

    def capture():
        return run("rex", "capture", "-s", session, "-b", initial, "--trim")

    def blocks():
        data = json.loads(run("rex", "block", "ls", "-s", session, "--json"))
        return [b for b in data["blocks"]
                if not b.get("label", "").startswith("rex-statusline:")]

    def click(row, col):
        data = f"\x1b[<0;{col};{row}M\x1b[<0;{col};{row}m"
        run("rex", "block", "call", "-s", session, "-b", initial,
            "write", json.dumps({"data": data}))

    try:
        wait(lambda: lua("vim.fn.getpid()"), "startup")
        lua("(function() vim.o.mouse='a';vim.o.showtabline=2;"
            "vim.api.nvim_buf_set_name(0,'FIRST_BUFFER');"
            "First=vim.api.nvim_get_current_tabpage();"
            "FirstBuf=vim.api.nvim_get_current_buf();"
            "vim.cmd('vsplit');vim.cmd('tabnew');"
            "Second=vim.api.nvim_get_current_tabpage();"
            "vim.api.nvim_buf_set_name(0,'SECOND_BUFFER');return true end)()")
        wait(lambda: len(blocks()) == 1, "inactive tab split removed")
        if not grid:
            wait(lambda: "Nvim tabs:" in capture() and "Buffers:" in capture(),
                 "separate tab and buffer rows")
        else:
            wait(lambda: "FIRST_BUFFER" in capture(), "grid tabline fallback")
        size = json.loads(run("rex", "block", "call", "-s", session,
                              "-b", initial, "size"))
        width = size["columns"]
        height = size["rows"]
        # External '<' chooses the previous handle; grid tabline starts with tab 1.
        click(height if grid else height - 1, 2 if grid else 1)
        wait(lambda: lua("tostring(vim.api.nvim_get_current_tabpage()==First)") == "true",
             "host click selects first Neovim tab")
        wait(lambda: len(blocks()) == 2, "first tab split restored")
        # No Rex tab was created or renamed.
        view = json.loads(run("rex", "session", "inspect", session, "--json"))
        assert len(view["windows"]) == 1
        if not grid:
            # Select next buffer from the primary renderer's buffer row.
            click(height, width)
            wait(lambda: lua("tostring(vim.api.nvim_get_current_buf()~=FirstBuf)") == "true",
                 "host buffer selection")
            assert lua("tostring(vim.api.nvim_get_current_tabpage()==First)") == "true"
            lua("(function() vim.api.nvim_buf_set_name(0,'RENAMED_BUFFER');return true end)()")
            wait(lambda: "RENAMED_BUFFER" in capture(), "rename refreshes chrome")
        for mode in (0, 1, 2, 0, 2):
            lua(f"(function() vim.o.showtabline={mode};return true end)()")
            if not grid:
                wait(lambda: ("Nvim tabs:" in capture()) == (mode != 0),
                     f"showtabline={mode}")
        lua("(function() vim.api.nvim_set_current_tabpage(Second);"
            "vim.cmd('tabclose');return true end)()")
        wait(lambda: len(blocks()) == 2, "close restores first tab topology")
        lua("(function() vim.o.showtabline=1;return true end)()")
        if not grid:
            wait(lambda: "Nvim tabs:" not in capture(), "single-tab auto-hide")
        assert lua("#vim.api.nvim_list_tabpages()") == "1"
        # CRUD typed through the actual renderer is reflected back by redraw.
        run("rex", "block", "call", "-s", session, "-b", initial,
            "write", json.dumps({"data": ":tabnew | file HOST_CREATED\r"}))
        wait(lambda: lua("#vim.api.nvim_list_tabpages()") == "2",
             "renderer keyboard creates Neovim tab")
        wait(lambda: "HOST_CREATED" in capture(), "host creation/name reflected")
        run("rex", "block", "call", "-s", session, "-b", initial,
            "write", json.dumps({"data": ":tabclose\r"}))
        wait(lambda: lua("#vim.api.nvim_list_tabpages()") == "1",
             "renderer keyboard closes Neovim tab")
        wait(lambda: len(blocks()) == 2, "host close restores split topology")
        print(f"PASS: {'grid fallback' if grid else 'external'} tabline, selection, showtabline, topology")
    finally:
        run("rex", "kill", session)


def main():
    with tempfile.TemporaryDirectory(prefix="rex-tabline-", dir="/tmp") as tmp:
        binary = str(Path(tmp) / "rex-nvim")
        run("go", "build", "-o", binary, "./cmd/rex-nvim")
        check(binary, tmp, False)
        check(binary, tmp, True)


if __name__ == "__main__":
    main()
