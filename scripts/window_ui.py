#!/usr/bin/env python3
"""Check native full-window chrome in a disposable Rex session."""

import json
import os
import tempfile
import time
from pathlib import Path

from smoke import ROOT, run, wait


def main():
    run("go", "build", "-o", str(ROOT / "build/rex-nvim"), "./cmd/rex-nvim")
    with tempfile.TemporaryDirectory(prefix="rex-window-ui-", dir="/tmp") as tmp:
        socket = str(Path(tmp) / "nvim.sock")
        created = json.loads(run(
            "rex", "new", "rex-nvim-window-ui", "--json", "--shell=none", "--",
            "/usr/bin/env", f"PATH={os.environ['PATH']}", str(ROOT / "build/rex-nvim"), "--clean", "-n", "--listen", socket,
        ))
        session = created["session_id"]

        def expr(code):
            return run("nvim", "--server", socket, "--remote-expr", code).strip()

        def view():
            return json.loads(run("rex", "session", "inspect", session, "--json"))["windows"][0]

        def capture(block):
            return run("rex", "capture", "-s", session, "-b", block, "--trim")

        def layers_with(text):
            return [
                layer for layer in view()["layers"]
                if any(text in capture(b["block_id"]) for b in layer["blocks"])
            ]

        try:
            wait(lambda: expr("getpid()"), "Neovim startup")
            expr('execute("set statusline=REX_GLOBAL_LEFT%=REX_GLOBAL_RIGHT")')
            expr("setline(1, 'REX_BUFFER_CONTENT')")
            expr('execute("vsplit | split")')
            wait(lambda: len(view()["layers"][0]["blocks"]) == 3, "three tiled splits")

            def shared_status():
                # No graphical client sizes disposable-session terminals. Match
                # full-width chrome to the virtual canvas through terminal RPC.
                width = int(expr("&columns"))
                for layer in view()["layers"]:
                    if layer["kind"] == "floating" and layer["bounds"][2] == 1:
                        for b in layer["blocks"]:
                            run("rex", "block", "call", "-s", session, "-b",
                                b["block_id"], "resize", json.dumps({"columns": width, "rows": 2}))
                layers = layers_with("REX_GLOBAL_LEFT")
                return (
                    len(layers) == 1 and layers[0]["kind"] == "floating"
                    and layers[0]["bounds"][0] == 0
                    and layers[0]["bounds"][2] == 1
                    and any(
                        "REX_GLOBAL_RIGHT" in capture(b["block_id"])
                        for b in layers[0]["blocks"]
                    )
                )

            wait(shared_status, "one continuous full-width statusline")
            wait(
                lambda: all("REX_BUFFER_CONTENT" in capture(b["block_id"])
                            for b in view()["layers"][0]["blocks"]),
                "buffer content in each tile",
            )
            for b in view()["layers"][0]["blocks"]:
                assert "REX_GLOBAL_" not in capture(b["block_id"])

            time.sleep(1)
            dimensions = expr("json_encode([&columns, &lines, &cmdheight])")
            # Fitting should converge, not repeatedly reserve shared chrome per pane.
            for _ in range(3):
                time.sleep(0.3)
                actual = expr("json_encode([&columns, &lines, &cmdheight])")
                assert actual == dimensions, (actual, dimensions)
            assert expr("&cmdheight") == "1"
            assert expr("&laststatus") == "3"
            expr('execute("set laststatus=2")')
            assert expr("&laststatus") == "3"

            expr('execute("set showtabline=2 tabline=REX_NATIVE_TABLINE")')
            wait(
                lambda: any(
                    layer["kind"] == "floating" and layer["bounds"][:3] == [0, 0, 1]
                    for layer in layers_with("REX_NATIVE_TABLINE")
                ),
                "native tabline spans the window top",
            )

            block = view()["layers"][0]["blocks"][0]["block_id"]
            run("rex", "send", "-s", session, "-b", block, ":REX_GLOBAL_COMMAND")
            wait(
                lambda: len(layers_with("REX_GLOBAL_COMMAND")) == 1
                and layers_with("REX_GLOBAL_COMMAND")[0]["kind"] == "floating"
                and layers_with("REX_GLOBAL_COMMAND")[0]["bounds"][2] == 1,
                "native command line spans all splits",
            )
            run("rex", "send-key", "-s", session, "-b", block, "escape")
            wait(lambda: not layers_with("REX_GLOBAL_COMMAND"), "cancel clears command")

            run("rex", "send", "-s", session, "-b", block,
                ":for i in range(1, 40) | echomsg 'REX_NATIVE_MESSAGE' | endfor")
            run("rex", "send-key", "-s", session, "-b", block, "enter")
            wait(
                lambda: any(layer["kind"] == "floating"
                            and layer["bounds"][2] == 1
                            and layer["bounds"][1] < 0.9
                            for layer in layers_with("REX_NATIVE_MESSAGE")),
                "blocking messages expand across the window",
            )
            run("rex", "send-key", "-s", session, "-b", block, "enter")
            wait(lambda: not layers_with("REX_NATIVE_MESSAGE"), "dismiss native message prompt")

            # Shared chrome is recoverable UI, not a Neovim window.
            status = layers_with("REX_GLOBAL_LEFT")[0]["blocks"][0]["block_id"]
            run("rex", "block", "close", "-s", session, status)
            wait(shared_status, "closed statusline renderer is recreated")
            assert expr("winnr('$')") == "3"
            expr('execute("set winbar=REX_NATIVE_WINBAR")')
            wait(lambda: len(layers_with("REX_NATIVE_WINBAR")) == 3,
                 "winbars use floating layers for each native window")
            assert all(layer["kind"] == "floating"
                       for layer in layers_with("REX_NATIVE_WINBAR"))
            expr('execute("only")')
            wait(lambda: len(view()["layers"][0]["blocks"]) == 1, "collapse to one tile")
            wait(shared_status, "global status survives layout changes")
            print("PASS: full-width statusline, tabline, command line, stable fitting, chrome recovery")
        except Exception:
            print(json.dumps(view(), indent=2))
            for layer in view()["layers"]:
                for b in layer["blocks"]:
                    print(b["block_id"], repr(capture(b["block_id"])))
            raise
        finally:
            run("rex", "kill", session)


if __name__ == "__main__":
    main()
