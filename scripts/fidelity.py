#!/usr/bin/env python3
"""Check real Neovim float restacking in a disposable Rex session."""

import json
import os
import tempfile
from pathlib import Path

from smoke import ROOT, run, wait


def main():
    binary = ROOT / "build/rex-nvim"
    run("go", "build", "-o", str(binary), "./cmd/rex-nvim")
    with tempfile.TemporaryDirectory(prefix="rex-nvim-fidelity-", dir="/tmp") as tmp:
        socket = str(Path(tmp) / "nvim.sock")
        created = json.loads(
            run(
                "rex",
                "new",
                "rex-nvim-fidelity",
                "--json",
                "--shell=none",
                "--",
                "/usr/bin/env",
                f"PATH={os.environ['PATH']}",
                str(binary),
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

        def float_order():
            windows = view()["windows"]
            assert len(windows) == 1
            names = []
            for layer in windows[0]["layers"]:
                for block in layer["blocks"]:
                    if block["block_id"] == initial:
                        continue
                    content = run(
                        "rex",
                        "capture",
                        "-s",
                        session,
                        "-b",
                        block["block_id"],
                        "--trim",
                    )
                    for name in ("FidelityA", "FidelityB"):
                        if name in content:
                            names.append(name)
            return names

        try:
            wait(lambda: lua("vim.fn.getpid()"), "Neovim startup")
            for name, zindex in (("FidelityA", 200), ("FidelityB", 50)):
                lua(
                    "(function() local a=vim.api; local b=a.nvim_create_buf(false,true);"
                    f"a.nvim_buf_set_lines(b,0,-1,false,{{'{name}'}});"
                    f"_G.{name}=a.nvim_open_win(b,false,{{relative='editor',"
                    f"row=2,col=2,width=16,height=3,focusable=false,zindex={zindex}}});"
                    "return true end)()"
                )
            wait(
                lambda: float_order() == ["FidelityB", "FidelityA"],
                "initial host layers follow zindex, not creation order",
            )
            lua(
                "(function() vim.api.nvim_win_set_config(FidelityA,{zindex=20});"
                "return true end)()"
            )
            wait(
                lambda: float_order() == ["FidelityA", "FidelityB"],
                "dynamic zindex restacks existing layers",
            )
            assert view()["windows"][0]["focused_block_id"] == initial
            lua(
                "(function() vim.api.nvim_win_set_config(FidelityA,{zindex=200});"
                "return true end)()"
            )
            wait(
                lambda: float_order() == ["FidelityB", "FidelityA"],
                "reverse restack",
            )
            assert view()["windows"][0]["focused_block_id"] == initial
            print("PASS: real float creation/dynamic/reverse stacking; focus preserved")
        finally:
            run("rex", "kill", session)


if __name__ == "__main__":
    main()
