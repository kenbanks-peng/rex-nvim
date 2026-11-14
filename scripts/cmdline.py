#!/usr/bin/env python3
"""External command-line and grid-fallback checks in disposable Rex sessions."""

import json
import os
import tempfile
from pathlib import Path

from smoke import run, wait


def check(binary, tmp, grid):
    socket = str(Path(tmp) / ("grid.sock" if grid else "external.sock"))
    created = json.loads(run(
        "rex", "new", "rex-cmdline-check", "--json", "--shell=none", "--",
        "/usr/bin/env", f"PATH={os.environ['PATH']}",
        f"REX_NVIM_CMDLINE={'grid' if grid else 'external'}",
        binary, "--clean", "-n", "--listen", socket))
    session = created["session_id"]

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

    def shown(text):
        return any(text in capture(b) for b in blocks())

    try:
        wait(lambda: lua("vim.fn.getpid()"), "startup")
        lua("(function() vim.o.hidden=true;vim.o.cmdheight=2;vim.cmd('vsplit');"
            "vim.cmd('enew');vim.api.nvim_buf_set_lines(0,0,-1,false,{'OWNER_BODY'});"
            "return true end)()")
        wait(lambda: len(blocks()) == 2, "two renderer panes")
        owner = wait(lambda: next((b for b in blocks() if "OWNER_BODY" in capture(b)), None),
                     "prompt owner")
        sibling = next(b for b in blocks() if b != owner)
        assert lua("tostring(vim.api.nvim_list_uis()[1].ext_cmdline)") == str(not grid).lower()
        write(owner, ":let g:host_cmd='界é🙂'")
        wait(lambda: shown("let g:host_cmd="), "Unicode colon command")
        if not grid:
            assert sum(capture(b).count("let g:host_cmd=") for b in blocks()) == 1
            assert "let g:host_cmd=" not in capture(sibling)
        if not grid:
            run("rex", "block", "call", "-s", session, "-b", owner, "resize",
                json.dumps({"columns": 32, "rows": 12}))
            run("rex", "block", "call", "-s", session, "-b", sibling, "resize",
                json.dumps({"columns": 36, "rows": 12}))
            wait(lambda: shown("let g:host_cmd="), "prompt after renderer resize")
        # Focus reports must not block the command editor on a window RPC.
        run("rex", "focus", "-s", session, sibling)
        write(sibling, "\x1b[DZ\r")
        wait(lambda: lua("vim.g.host_cmd or ''") == "界é🙂Z", "edit/execute after sibling focus")
        write(owner, "/OWNER")
        wait(lambda: shown("/OWNER"), "search prompt")
        write(owner, "\x03")
        wait(lambda: not shown("/OWNER"), "search cancellation")
        write(owner, ":let g:nested_cmd=")
        write(owner, "\x12=1+2")
        wait(lambda: shown("1+2"), "nested expression prompt")
        write(owner, "\r\r")
        wait(lambda: lua("tostring(vim.g.nested_cmd)") == "3", "nested prompt acceptance")
        write(owner, ":let g:prompt_answer=input('问: ', '界é')\r")
        wait(lambda: shown("问: "), "input() prompt")
        write(owner, "\x7fz\r")
        wait(lambda: lua("vim.g.prompt_answer or ''") == "界z", "Unicode input() edit")
        write(owner, ":function RNHostBlock()\r")
        if not grid:
            wait(lambda: shown("function RNHostBlock()"), "command block show")
        else:
            # The native fallback clips command history to its message rows.
            wait(lambda: lua("vim.fn.getcmdtype()") == ":", "native function prompt")
        write(owner, "echo 'BLOCK_BODY'")
        wait(lambda: shown("BLOCK_BODY"), "command block editing")
        write(owner, "\r")
        if not grid:
            wait(lambda: shown("BLOCK_BODY"), "command block append")
        write(owner, "endfunction\r")
        wait(lambda: lua("vim.fn.exists('*RNHostBlock')") == "1", "command block acceptance")
        wait(lambda: not shown("BLOCK_BODY"), "command block hide")
        write(owner, ":function RNHostCancelled()\r")
        if not grid:
            wait(lambda: shown("function RNHostCancelled()"), "second block")
        else:
            wait(lambda: lua("vim.fn.getcmdtype()") == ":", "native second block")
        write(owner, "\x03")
        wait(lambda: lua("vim.api.nvim_get_mode().mode") == "n", "command block abort")
        wait(lambda: not shown("RNHostCancelled"), "command block abort repaint")
        if grid:
            write(owner, ":let g:resize_check=1")
            wait(lambda: shown("let g:resize_check=1"), "grid resize prompt")
            for b in (owner, sibling):
                run("rex", "block", "call", "-s", session, "-b", b, "resize",
                    json.dumps({"columns": 36, "rows": 12}))
            wait(lambda: shown("let g:resize_check=1"), "grid prompt survives resize")
            write(owner, "\r")
            wait(lambda: lua("tostring(vim.g.resize_check)") == "1", "grid command executes after resize")
        assert len(blocks()) == 2, "command widget created a pane"
        print(f"PASS: {'grid fallback' if grid else 'external'} command lines, Unicode editing, nested/input prompts, blocks, focus, resize, cancellation")
    finally:
        run("rex", "kill", session)


def main():
    with tempfile.TemporaryDirectory(prefix="rex-cmdline-", dir="/tmp") as tmp:
        binary = str(Path(tmp) / "rex-nvim")
        run("go", "build", "-o", binary, "./cmd/rex-nvim")
        check(binary, tmp, False)
        check(binary, tmp, True)


if __name__ == "__main__":
    main()
