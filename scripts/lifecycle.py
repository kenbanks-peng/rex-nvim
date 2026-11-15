#!/usr/bin/env python3
"""Exercise lifecycle policy using only disposable Rex sessions."""

import json
import os
import shlex
import signal
import tempfile
from pathlib import Path

from smoke import run, wait


def main():
    with tempfile.TemporaryDirectory(prefix="rex-lifecycle-", dir="/tmp") as tmp:
        binary = str(Path(tmp) / "rex-nvim")
        run("go", "build", "-o", binary, "./cmd/rex-nvim")
        for failure in (False, True):
            exercise(binary, tmp, failure)


def exercise(binary, tmp, failure):
    socket = str(Path(tmp) / ("failure.sock" if failure else "normal.sock"))
    # Keep the primary shell alive to inspect its terminal after exit.
    command = (
        shlex.join([binary, "--clean", "-n", "--listen", socket])
        + "; printf '\\nLIFECYCLE_RETURN\\n'; sleep 60"
    )
    created = json.loads(
        run(
            "rex",
            "new",
            "rex-lifecycle",
            "--json",
            "--shell=none",
            "--",
            "/usr/bin/env",
            f"PATH={os.environ['PATH']}",
            "/bin/sh",
            "-c",
            command,
        )
    )
    session = created["session_id"]
    initial = created["initial_windows"][0]["block_ids"][0]

    def lua(code):
        return run(
            "nvim",
            "--server",
            socket,
            "--remote-expr",
            "luaeval(" + json.dumps(code) + ")",
        ).strip()

    def blocks():
        view = json.loads(run("rex", "session", "inspect", session, "--json"))
        return [
            b["block_id"]
            for w in view["windows"]
            for layer in w["layers"]
            if layer["kind"] == "tiled" or layer["bounds"][2] < 0.999
            for b in layer["blocks"]
            if not b.get("label", "").startswith("rex-statusline:")
        ]

    def capture(block):
        return run("rex", "capture", "-s", session, "-b", block, "--trim")

    def processes():
        return [
            (int(line.strip().split(None, 1)[0]), line.strip().split(None, 1)[1])
            for line in run("ps", "-axo", "pid=,command=").splitlines()
            if line.strip() and len(line.strip().split(None, 1)) == 2
        ]

    def secondary_pid():
        return next(
            (pid for pid, cmd in processes() if cmd.startswith(binary + " _pane ") and not cmd.rsplit(" ", 1)[-1].startswith("-")),
            None,
        )

    def assert_restored(block):
        modes = json.loads(
            run("rex", "block", "call", "-s", session, "-b", block, "modes")
        )
        assert modes["screen"] == "primary", modes
        disabled = {1000, 1002, 1003, 1004, 1006, 2004, 1049}
        assert all(not m["set"] for m in modes["modes"] if m["number"] in disabled), (
            modes
        )
        assert next(m["set"] for m in modes["modes"] if m["number"] == 25), modes

    stopped = None
    try:
        nvpid = int(wait(lambda: lua("vim.fn.getpid()"), "Neovim startup"))
        lua(
            "(function() vim.api.nvim_buf_set_lines(0,0,-1,false,{'UNSAVED_LIFECYCLE'}); vim.cmd('vsplit'); return true end)()"
        )
        wait(lambda: len(blocks()) == 2, "two panes")
        secondary = next(b for b in blocks() if b != initial)
        renderer = wait(secondary_pid, "secondary renderer")
        original = set(blocks())
        os.kill(renderer, signal.SIGTSTP)
        stopped = renderer
        wait(
            lambda: "T" in run("ps", "-o", "state=", "-p", str(renderer)),
            "renderer stopped",
        )
        assert_restored(secondary)
        # More than the writer's two-second deadline, with active redraws.
        lua(
            "(function() vim.g.lifecycle_tick=0; vim.fn.timer_start(50,function() vim.g.lifecycle_tick=vim.g.lifecycle_tick+1; vim.api.nvim_buf_set_lines(0,1,-1,false,{tostring(vim.g.lifecycle_tick)}); vim.cmd('redraw!') end,{['repeat']=65}); return true end)()"
        )
        wait(
            lambda: lua("vim.g.lifecycle_tick") == "65",
            "backend runs while renderer stopped",
        )
        assert set(blocks()) == original, "suspension replaced a pane"
        assert "UNSAVED_LIFECYCLE" in capture(initial)
        os.kill(renderer, signal.SIGCONT)
        stopped = None
        wait(lambda: "UNSAVED_LIFECYCLE" in capture(secondary), "resume repaint")
        assert int(lua("vim.fn.getpid()")) == nvpid

        # Neovim suspend is declined, never a backend/group signal.
        lua("(function() vim.cmd('suspend'); return true end)()")
        assert int(lua("vim.fn.getpid()")) == nvpid
        assert set(blocks()) == original

        # A renderer exit can auto-close its Rex block before recovery.
        # Either recovery or intentional-close reconciliation must settle.
        os.kill(renderer, signal.SIGTERM)
        wait(lambda: secondary not in blocks(), "disconnected renderer removed")
        wait(
            lambda: int(lua("#vim.api.nvim_list_wins()")) == len(blocks()),
            "disconnect reconciled",
        )
        wait(
            lambda: all("UNSAVED_LIFECYCLE" in capture(b) for b in blocks()),
            "unsaved content survives disconnect",
        )
        assert lua("vim.bo.modified") == "true"
        assert int(lua("vim.fn.getpid()")) == nvpid

        if len(blocks()) == 1:
            lua("(function() vim.cmd('vsplit'); return true end)()")
            wait(lambda: len(blocks()) == 2, "secondary for teardown")
        # Include a float to exercise layer teardown as well as tiled panes.
        lua(
            "(function() vim.api.nvim_open_win(vim.api.nvim_create_buf(false,true),false,{relative='editor',row=1,col=1,width=12,height=2}); return true end)()"
        )
        wait(lambda: len(blocks()) == 3, "float renderer")
        if failure:
            os.kill(nvpid, signal.SIGKILL)
        else:
            lua(
                "(function() vim.schedule(function() vim.cmd('qa!') end); return true end)()"
            )
        wait(lambda: "LIFECYCLE_RETURN" in capture(initial), "primary caller resumed")
        wait(lambda: blocks() == [initial], "secondary panes and float removed")
        assert_restored(initial)
        wait(
            lambda: not any(cmd.startswith(binary + " _") for _, cmd in processes()),
            "backend and helpers exited",
        )
        print(
            "PASS",
            "child failure" if failure else "normal exit",
            "suspend/resume, declined UI suspend, renderer disconnect, pane cleanup",
        )
    finally:
        if stopped:
            os.kill(stopped, signal.SIGCONT)
        run("rex", "kill", session)


if __name__ == "__main__":
    main()
