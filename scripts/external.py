#!/usr/bin/env python3
"""Check external-window layers in a disposable Rex session."""

import json
import os
import signal
import tempfile
from pathlib import Path

from smoke import run, wait


def main():
    with tempfile.TemporaryDirectory(prefix="rex-external-", dir="/tmp") as tmp:
        binary = str(Path(tmp) / "rex-nvim")
        socket = str(Path(tmp) / "nvim.sock")
        run("go", "build", "-o", binary, "./cmd/rex-nvim")
        created = json.loads(
            run(
                "rex",
                "new",
                "rex-external",
                "--json",
                "--shell=none",
                "--",
                "/usr/bin/env",
                f"PATH={os.environ['PATH']}",
                binary,
                "--clean", "-n",
                "--listen",
                socket,
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

        def view():
            return json.loads(run("rex", "session", "inspect", session, "--json"))

        def layers():
            return [
                l
                for w in view()["windows"]
                for l in w["layers"]
                if any(
                    not b.get("label", "").startswith("rex-statusline:")
                    for b in l["blocks"]
                )
                and l.get("kind") != "tiled"
                and not any(b["block_id"] == initial for b in l["blocks"])
            ]

        def capture(block):
            return run("rex", "capture", "-s", session, "-b", block, "--trim")

        def external():
            return next(
                (
                    l
                    for l in layers()
                    if any(
                        "EXTERNAL_UNSAVED" in capture(b["block_id"])
                        for b in l["blocks"]
                    )
                ),
                None,
            )

        def size():
            return lua(
                "table.concat({vim.api.nvim_win_get_width(Ext),"
                "vim.api.nvim_win_get_height(Ext)},',')"
            )

        try:
            wait(lambda: lua("vim.fn.getpid()"), "startup")
            baseline = lua("vim.inspect(vim.fn.winlayout())")
            lua(
                "(function() local a=vim.api; local b=a.nvim_create_buf(true,false);"
                "a.nvim_buf_set_lines(b,0,-1,false,{'EXTERNAL_UNSAVED'});"
                "vim.o.hidden=false; Ext=a.nvim_open_win(b,true,"
                "{external=true,width=32,height=8}); return true end)()"
            )
            layer = wait(external, "external renderer content")
            block = layer["blocks"][0]["block_id"]
            assert len(layers()) == 1, view()
            run("rex", "focus", "-s", session, initial)
            wait(
                lambda: lua("tostring(vim.api.nvim_get_current_win()~=Ext)") == "true",
                "tiled focus",
            )
            run("rex", "focus", "-s", session, block)
            wait(
                lambda: lua("tostring(vim.api.nvim_get_current_win()==Ext)") == "true",
                "external focus",
            )
            bounds = [0.1, 0.15, 0.7, 0.6]
            run(
                "rex",
                "api",
                "call",
                "-s",
                session,
                "session.resize_layer",
                json.dumps({"layer_id": layer["layer_id"], "bounds": bounds}),
            )
            # Detached sessions have no graphical client to translate fractional
            # bounds into cells. Exercise that client's terminal resize RPC too.
            run(
                "rex",
                "block",
                "call",
                "-s",
                session,
                "-b",
                block,
                "resize",
                json.dumps({"columns": 47, "rows": 11}),
            )
            wait(lambda: size() == "47,11", "terminal resize reaches external grid")
            assert lua("vim.inspect(vim.fn.winlayout())") == baseline
            assert external()["bounds"] == bounds

            # A window-relative float stays composed inside the external pane.
            lua(
                "(function() local a=vim.api; local b=a.nvim_create_buf(false,true);"
                "a.nvim_buf_set_lines(b,0,-1,false,{'CHILD_POPUP'});"
                "Child=a.nvim_open_win(b,false,{relative='win',win=Ext,"
                "row=2,col=1,width=14,height=2}); return true end)()"
            )
            wait(lambda: "CHILD_POPUP" in capture(block), "external popup composition")
            assert len(layers()) == 1, view()
            lua("(function() vim.api.nvim_win_close(Child,true); return true end)()")

            # Kill only this test's renderer. Modified-buffer protection must
            # preserve the external window even if Rex auto-removes its block.
            process = json.loads(
                run("rex", "block", "call", "-s", session, "-b", block, "process")
            )
            renderer = process.get("foreground") or process.get("program")
            assert renderer, process
            os.kill(renderer["pid"], signal.SIGTERM)
            wait(
                lambda: all(
                    b["block_id"] != block for l in layers() for b in l["blocks"]
                ),
                "lost renderer removed",
            )
            replacement = wait(external, "external recovery preserves unsaved buffer")
            assert lua("tostring(vim.api.nvim_win_is_valid(Ext))") == "true"
            assert (
                lua("tostring(vim.bo[vim.api.nvim_win_get_buf(Ext)].modified)")
                == "true"
            )
            assert lua("vim.inspect(vim.fn.winlayout())") == baseline
            block = replacement["blocks"][0]["block_id"]
            run("rex", "block", "close", "-s", session, block)
            wait(external, "intentional close rejects modified-buffer loss")
            lua(
                "(function() vim.bo[vim.api.nvim_win_get_buf(Ext)].modified=false;"
                "vim.api.nvim_win_hide(Ext); return true end)()"
            )
            wait(lambda: len(layers()) == 0, "hide removes external layer")
            assert lua("vim.inspect(vim.fn.winlayout())") == baseline
            assert initial in capture_ids(view())
            print(
                "PASS: external open/focus/resize/popup/recovery/protected close/hide"
            )
        finally:
            run("rex", "kill", session)


def capture_ids(snapshot):
    return [
        b["block_id"]
        for w in snapshot["windows"]
        for l in w["layers"]
        for b in l["blocks"]
    ]


if __name__ == "__main__":
    main()
