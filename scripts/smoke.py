#!/usr/bin/env python3
"""Exercise a disposable Rex session using real Neovim and renderer processes."""

import argparse
import json
import os
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def run(*args):
    return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT, timeout=15)


def wait(check, description):
    deadline = time.monotonic() + 10
    last = None
    while time.monotonic() < deadline:
        try:
            last = check()
            if last:
                return last
        except (subprocess.CalledProcessError, ValueError) as error:
            last = str(error)
        time.sleep(0.1)
    raise AssertionError(f"{description}: {last}")


def main():
    build = ["go", "build"]
    if os.environ.get("REX_NVIM_RACE"):
        build.append("-race")
    run(*build, "-o", str(ROOT / "build/rex-nvim"), "./cmd/rex-nvim")
    with tempfile.TemporaryDirectory(prefix="rex-nvim-test-", dir="/tmp") as tmp:
        socket = str(Path(tmp) / "nvim.sock")
        created = json.loads(
            run(
                "rex",
                "new",
                "rex-nvim-smoke",
                "--json",
                "--shell=none",
                "--",
                "/usr/bin/env",
                f"PATH={os.environ['PATH']}",
                str(ROOT / "build/rex-nvim"),
                "--clean", "-n",
                "--listen",
                socket,
            )
        )
        session = created["session_id"]
        initial = created["initial_windows"][0]["block_ids"][0]

        def expr(code):
            return run("nvim", "--server", socket, "--remote-expr", code)

        def blocks():
            result = json.loads(run("rex", "block", "ls", "-s", session, "--json"))
            return [
                b["block_id"]
                for b in result["blocks"]
                if not b.get("label", "").startswith("rex-statusline:")
            ]

        def capture(block):
            return run("rex", "capture", "-s", session, "-b", block, "--trim")

        try:
            wait(lambda: expr("getpid()"), "Neovim startup")
            ui = json.loads(expr("json_encode(nvim_list_uis()[0])"))
            cmdline = os.environ.get("REX_NVIM_CMDLINE") != "grid"
            expected = {
                "rgb": True, "ext_linegrid": True, "ext_multigrid": True,
                "ext_tabline": os.environ.get("REX_NVIM_TABLINE") != "grid",
                "ext_popupmenu": os.environ.get("REX_NVIM_POPUPMENU") != "grid",
                "ext_cmdline": cmdline,
                "ext_messages": cmdline and os.environ.get("REX_NVIM_MESSAGES") != "grid",
                "ext_wildmenu": False, "ext_hlstate": False, "ext_termcolors": False,
            }
            for option, enabled in expected.items():
                assert ui.get(option, False) == enabled, (option, ui, expected)
            print("Negotiated UI:", json.dumps(expected, sort_keys=True), flush=True)
            expr("setline(1, 'REX_SHARED_BUFFER')")
            wait(lambda: "REX_SHARED_BUFFER" in capture(initial), "initial rendering")
            expr('execute("vsplit")')
            wait(lambda: len(blocks()) == 2, "vsplit creates a Rex block")
            wait(
                lambda: all("REX_SHARED_BUFFER" in capture(b) for b in blocks()),
                "shared buffer renders in both blocks",
            )
            assert expr("len(nvim_list_bufs())").strip() == "1"
            # Noice opens a Lua editor-relative border and a nested input.
            owner = expr("nvim_get_current_win()").strip()
            popup = expr(
                "luaeval(\"vim.api.nvim_open_win(vim.api.nvim_create_buf(false,true),false,"
                "{relative='editor',row=2,col=(vim.o.columns-64)/2,"
                "width=64,height=3,focusable=false})\")"
            ).strip()
            popup_input = expr(
                "luaeval(\"vim.api.nvim_open_win(vim.api.nvim_create_buf(false,true),false,"
                "{relative='win',win="
                + popup
                + ",row=1,col=2,width=60,height=1,focusable=false,zindex=200})\")"
            ).strip()
            assert expr("nvim_win_get_config(" + popup + ").relative").strip() == "editor"
            popup_text = "REX_POPUP_LEFT".ljust(45, ".") + "REX_POPUP_RIGHT"
            expr(
                "nvim_buf_set_lines(nvim_win_get_buf("
                + popup_input
                + "), 0, -1, v:false, ["
                + json.dumps(popup_text)
                + "])"
            )
            def popup_in_one_pane():
                frames = [capture(b) for b in blocks()]
                return (
                    sum(
                        "REX_POPUP_LEFT" in frame and "REX_POPUP_RIGHT" in frame
                        for frame in frames
                    ) == 1
                    and sum("REX_POPUP_" in frame for frame in frames) == 1
                )

            wait(lambda: len(blocks()) == 4, "nonfocusable popups create Rex floating blocks")
            wait(popup_in_one_pane, "nested popup renders only in its floating block")
            assert expr("nvim_get_current_win()").strip() == owner, "overlay stole editor focus"
            view = json.loads(run("rex", "session", "inspect", session, "--json"))
            tiled = [
                b["block_id"]
                for layer in view["windows"][0]["layers"]
                if layer["kind"] == "tiled"
                for b in layer["blocks"]
            ]
            assert all("REX_POPUP_" not in capture(b) for b in tiled)
            overlay_block = next(b for b in blocks() if "REX_POPUP_LEFT" in capture(b))
            run("rex", "focus", "-s", session, overlay_block)
            wait(
                lambda: json.loads(run("rex", "session", "inspect", session, "--json"))[
                    "windows"
                ][0]["focused_block_id"] != overlay_block,
                "display-only Rex float returns focus to the editor",
            )
            assert expr("nvim_get_current_win()").strip() == owner
            expr("nvim_win_close(" + popup_input + ", v:true)")
            expr("nvim_win_close(" + popup + ", v:true)")
            wait(lambda: len(blocks()) == 2, "overlay close removes Rex layers")
            expr('execute("vertical resize 30")')

            def split_ratio():
                view = json.loads(run("rex", "session", "inspect", session, "--json"))
                return view["windows"][0]["layers"][0]["layout"]["split"]["ratio"]

            wait(
                lambda: abs(split_ratio() - 0.5) > 0.05,
                "Neovim resize moves Rex split border",
            )
            expr('execute("wincmd L")')
            wait(
                lambda: "REX_SHARED_BUFFER" in capture(initial),
                "window rearrangement preserves rendering",
            )
            expr('execute("split")')
            wait(lambda: len(blocks()) == 3, "horizontal split creates a Rex block")
            expr('execute("close")')
            wait(lambda: len(blocks()) == 2, "close removes only its block")
            secondary = next(b for b in blocks() if b != initial)
            run("rex", "focus", "-s", session, secondary)
            run("rex", "send", "-s", session, "-b", secondary, "A_TYPED")
            run("rex", "send-key", "-s", session, "-b", secondary, "escape")
            time.sleep(0.1)
            wait(
                lambda: "_TYPED" in expr("getline(1)"),
                "keyboard input reaches shared editor",
            )
            expected = expr("getline(1)").strip()
            run("rex", "block", "close", "-s", session, secondary)
            wait(
                lambda: expr("winnr('$')").strip() == "1",
                "Rex close closes Neovim window",
            )
            floating = expr(
                "nvim_open_win(nvim_create_buf(v:false, v:true), v:true, "
                "{'relative':'editor','row':3,'col':5,'width':30,'height':5})"
            ).strip()
            expr(
                "nvim_buf_set_lines(nvim_win_get_buf("
                + floating
                + "), 0, -1, v:false, ['REX_FLOAT'])"
            )
            wait(lambda: len(blocks()) == 2, "focusable float creates a Rex block")
            wait(
                lambda: any(
                    "REX_FLOAT" in capture(b) for b in blocks() if b != initial
                ),
                "float renders in its block",
            )
            wait(
                lambda: "REX_FLOAT" not in capture(initial),
                "external float is not duplicated in the tiled pane",
            )
            assert json.loads(
                expr(
                    "json_encode([nvim_win_get_width("
                    + floating
                    + "), nvim_win_get_height("
                    + floating
                    + ")])"
                )
            ) == [30, 5], "renderer enlarged the popup beyond its requested size"
            expr("nvim_win_close(" + floating + ", v:true)")
            wait(lambda: len(blocks()) == 1, "float close removes its Rex layer")
            expr('execute("tabnew")')
            wait(lambda: len(blocks()) == 1, "tab switch preserves invoking block")
            expr('execute("tabprevious")')
            wait(lambda: expected in capture(initial), "tab return restores buffer")
            # A Rex close must not silently discard a unique modified buffer.
            expr('execute("set nohidden | vsplit | enew")')
            expr("setline(1, 'UNSAVED_GUARD')")
            guard_window = expr("nvim_get_current_win()").strip()
            wait(lambda: len(blocks()) == 2, "modified-buffer split")
            guarded = next(b for b in blocks() if b != initial)
            run("rex", "block", "close", "-s", session, guarded)
            wait(
                lambda: len(blocks()) == 2 and guarded not in blocks(),
                "rejected modified-buffer close recreates renderer",
            )
            wait(
                lambda: any("UNSAVED_GUARD" in capture(b) for b in blocks()),
                "modified buffer remains visible",
            )
            expr('execute("set hidden")')
            expr("nvim_win_close(" + guard_window + ", v:false)")
            wait(
                lambda: len(blocks()) == 1,
                "close restored pane with hidden buffers allowed",
            )
            expr('execute("vsplit | split | botright split")')
            wait(lambda: len(blocks()) == 4, "nested and spanning splits")

            def layout_direction():
                view = json.loads(run("rex", "session", "inspect", session, "--json"))
                return view["windows"][0]["layers"][0]["layout"]["split"]["direction"]

            wait(lambda: layout_direction() == "vertical", "bottom split spans editor")
            expr('execute("wincmd H")')
            wait(
                lambda: layout_direction() == "horizontal",
                "move window spans editor edge",
            )
            expr('execute("only")')
            wait(lambda: len(blocks()) == 1, "only preserves invoking block")
            wait(lambda: expected in capture(initial), "nested layout retains buffer")
            # Closing the launching block must not kill the shared backend.
            expr('execute("vsplit")')
            wait(lambda: len(blocks()) == 2, "split before original-block closure")
            survivor = next(b for b in blocks() if b != initial)
            pid = expr("getpid()")
            run("rex", "block", "close", "-s", session, initial)
            wait(lambda: len(blocks()) == 1, "original block closes independently")
            wait(lambda: expr("winnr('$')").strip() == "1", "original window closes")
            assert expr("getpid()") == pid
            wait(
                lambda: expected in capture(survivor), "survivor retains shared editor"
            )
            # Quitting may close the RPC channel before the expression reply.
            subprocess.run(
                ["nvim", "--server", socket, "--remote-expr", 'execute("qa!")'],
                check=False,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            wait(lambda: not Path(socket).exists(), "quit stops Neovim")
            print(
                "PASS: rendering, shared buffers, splits, resize, rearrangement, input, floats, tabs, original-block closure, quit"
            )
        except Exception:
            print(run("rex", "session", "inspect", session, "--json"))
            for b in blocks():
                print(b, repr(capture(b)))
            subprocess.run(
                [
                    "nvim",
                    "--server",
                    socket,
                    "--remote-expr",
                    "string(winlayout())",
                ],
                check=False,
                timeout=5,
            )
            raise
        finally:
            run("rex", "kill", session)


def protocol_suite():
    """Run all host regressions serially; each owns and cleans its own session."""
    environment = os.environ.copy()
    # Do not let caller preferences silently remove coverage from another check.
    for widget in ("TABLINE", "POPUPMENU", "CMDLINE", "MESSAGES"):
        environment[f"REX_NVIM_{widget}"] = "external"
    for script in ("smoke", "tabline", "popupmenu", "cmdline", "messages",
                   "external", "fidelity", "host_events", "lifecycle"):
        print(f"CHECK: {script}", flush=True)
        subprocess.run(
            [sys.executable, str(ROOT / "scripts" / f"{script}.py")],
            cwd=ROOT, env=environment, check=True, timeout=240,
        )
    environment["REX_NVIM_HOST_TEST"] = "1"
    subprocess.run(
        ["go", "test", "./internal/screen", "-run", "TestRex", "-count=1", "-v"],
        cwd=ROOT, env=environment, check=True, timeout=120,
    )
    print("PASS: complete UI protocol host suite", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--protocol", action="store_true",
                        help="run all capability host checks, including grid fallbacks")
    args = parser.parse_args()
    if args.protocol:
        protocol_suite()
    else:
        main()
