#!/usr/bin/env python3
"""External messages and native fallbacks in disposable Rex sessions."""

import json
import os
import tempfile
from pathlib import Path

from smoke import run, wait


def check(binary, tmp, mode):
    socket = str(Path(tmp) / f"{mode}.sock")
    created = json.loads(run(
        "rex", "new", "rex-messages-check", "--json", "--shell=none", "--",
        "/usr/bin/env", f"PATH={os.environ['PATH']}",
        f"REX_NVIM_MESSAGES={'external' if mode == 'external' else 'grid'}",
        f"REX_NVIM_CMDLINE={'grid' if mode == 'all-grid' else 'external'}",
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

    def resize(block):
        run("rex", "block", "call", "-s", session, "-b", block, "resize",
            json.dumps({"columns": 48, "rows": 14}))

    try:
        wait(lambda: lua("vim.fn.getpid()"), "startup")
        lua("(function() vim.o.hidden=true;vim.o.laststatus=0;vim.o.showmode=true;"
            "vim.o.showcmd=true;vim.o.ruler=true;vim.cmd('vsplit');vim.cmd('enew');"
            "vim.api.nvim_buf_set_lines(0,0,-1,false,{'MESSAGE_OWNER'});return true end)()")
        wait(lambda: len(blocks()) == 2, "two panes")
        owner = wait(lambda: next((b for b in blocks() if "MESSAGE_OWNER" in capture(b)), None),
                     "owner pane")
        sibling = next(b for b in blocks() if b != owner)
        assert lua("tostring(vim.api.nvim_list_uis()[1].ext_messages)") == str(mode == "external").lower()
        assert lua("tostring(vim.api.nvim_list_uis()[1].ext_cmdline)") == str(mode != "all-grid").lower()
        local_history = lua("vim.fn.has('nvim-0.12')") == "1"
        if mode == "external":
            assert lua("tostring(vim.o.cmdheight)") == "0", "layout restored native message allocation"

        write(owner, ":echo 'HOST_ECHO_界é🙂'\r")
        wait(lambda: shown("HOST_ECHO_界é🙂"), "echo with Unicode")
        if mode == "external":
            assert sum(capture(b).count("HOST_ECHO_") for b in blocks()) == 1
            resize(owner)
            if local_history:
                wait(lambda: shown("HOST_ECHO_"), "message survives resize")
            else:
                # API 13 clears native transient messages during root resize.
                # Verify new output still arrives instead of overriding msg_clear.
                write(owner, ":echo 'AFTER_RESIZE_界é🙂'\r")
                wait(lambda: shown("AFTER_RESIZE_界é🙂"), "API 13 output after resize")
        write(owner, ":RNMessageMissingCommand\r")
        wait(lambda: shown("E492"), "error message")
        write(owner, "\x1bi")
        wait(lambda: shown("INSERT"), "mode update")
        write(owner, "\x1b")
        wait(lambda: not shown("INSERT"), "mode cleared")
        if mode == "external":
            write(owner, "2d")
            wait(lambda: shown("2d"), "showcmd update")
            write(owner, "\x1b")

        write(owner, ':let g:host_confirm=confirm("HOST_CONFIRM", "&Yes\\n&No", 2)\r')
        wait(lambda: shown("(Y)es"), "confirmation choices")
        run("rex", "focus", "-s", session, sibling)
        resize(owner)
        resize(sibling)
        wait(lambda: shown("(Y)es"), "confirmation survives focus and resize")
        write(sibling, "y")
        wait(lambda: lua("tostring(vim.g.host_confirm)") == "1", "confirmation answer from sibling")

        if mode == "external" and local_history:
            for i in range(25):
                lua(f"(function() vim.api.nvim_echo({{{{'HISTORY_{i:03d}'}}}},true,{{}});return true end)()")
            write(owner, ":messages\r")
            wait(lambda: shown("History:") and shown("HISTORY_024"), "history snapshot")
            assert sum(capture(b).count("History:") for b in blocks()) == 1
            write(sibling, "\x1b[5~")
            wait(lambda: shown("HISTORY_010") and not shown("HISTORY_024"), "history page up")
            resize(owner)
            wait(lambda: shown("History:"), "history survives resize")
            write(sibling, "\r")
            wait(lambda: not shown("History:"), "history dismissal")
        elif mode == "external":
            write(owner, ":messages\r")
            wait(lambda: shown("Press ENTER") and not shown("History:"),
                 "API 13 native history prompt is not covered by local viewer")
            run("rex", "focus", "-s", session, sibling)
            resize(owner)
            resize(sibling)
            wait(lambda: shown("Press ENTER"), "API 13 history prompt survives resize")
            write(sibling, "\r")
            wait(lambda: not shown("Press ENTER"), "API 13 history prompt dismissal")
        else:
            write(owner, ':set nomore\r:echo join(map(range(1,40), "string(v:val)"), "\\n")\r')
            wait(lambda: shown("Press ENTER"), "native hit-enter")
            run("rex", "focus", "-s", session, sibling)
            resize(owner)
            resize(sibling)
            wait(lambda: shown("Press ENTER"), "native prompt survives resize")
            write(sibling, "\r")
            wait(lambda: not shown("Press ENTER"), "native prompt dismissal")

        write(sibling, "iAFTER_MESSAGES\x1b")
        wait(lambda: shown("AFTER_MESSAGES"), "normal sibling editing resumes")
        assert len(blocks()) == 2, "messages created a pane"
        print(f"PASS: {mode}: echo/errors, mode/status, prompts, pane focus, resize, dismissal"
              + (", Unicode and paged history" if mode == "external" and local_history else ", native hit-enter"))
    except Exception:
        for b in blocks():
            print(b, capture(b))
        raise
    finally:
        run("rex", "kill", session)


def main():
    with tempfile.TemporaryDirectory(prefix="rex-messages-", dir="/tmp") as tmp:
        binary = str(Path(tmp) / "rex-nvim")
        run("go", "build", "-o", binary, "./cmd/rex-nvim")
        for mode in ("external", "message-grid", "all-grid"):
            check(binary, tmp, mode)


if __name__ == "__main__":
    main()
