#!/usr/bin/env python3
"""Verify pane-local Neovim host events in a disposable Rex session."""

import json
import os
import queue
import subprocess
import tempfile
import threading
import time
from pathlib import Path
from urllib.parse import unquote, urlparse

from smoke import ROOT, run, wait


def quote(value):
    return '"' + "".join(f"\\{byte:03d}" for byte in value.encode()) + '"'


def main():
    with tempfile.TemporaryDirectory(prefix="rex-nvim-host-", dir="/tmp") as tmp:
        binary = str(Path(tmp) / "rex-nvim")
        run("go", "build", "-o", binary, "./cmd/rex-nvim")
        socket = str(Path(tmp) / "nvim.sock")
        created = json.loads(
            run(
                "rex",
                "new",
                "rex-nvim-host-events",
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
        events = None

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

        def call(block, method):
            return json.loads(
                run("rex", "block", "call", "-s", session, "-b", block, method)
            )

        try:
            wait(lambda: lua("vim.fn.getpid()"), "Neovim startup")
            lua("(function() vim.cmd('vsplit'); return true end)()")
            wait(lambda: len(blocks()) == 2, "two editor panes")
            owned = blocks()

            def process_cwds():
                cwds = {
                    b: (call(b, "process").get("child") or {}).get("cwd") for b in owned
                }
                return cwds if all(cwds.values()) else False

            original_cwds = wait(process_cwds, "renderer process cwd available")
            title = "Title ';$(touch SHOULD_NOT_EXIST)\x1b]52;c;payload\a"
            safe_title = "".join(
                c for c in title if not (ord(c) < 32 or 127 <= ord(c) <= 159)
            )
            directory = str(Path(tmp) / "cwd ';$(touch SHOULD_NOT_EXIST)\n\x1b\\?#%")
            os.mkdir(directory)
            lua(
                "(function() vim.o.titlestring="
                + quote(title)
                + "; vim.o.title=true; vim.api.nvim_set_current_dir("
                + quote(directory)
                + "); vim.cmd('redraw!'); return true end)()"
            )

            def metadata_ok():
                for block in owned:
                    if call(block, "title")["title"] != safe_title:
                        return False
                    reported = call(block, "pwd").get("pwd", "")
                    if unquote(urlparse(reported).path) != directory:
                        return False
                return True

            wait(metadata_ok, "host title and hostile encoded editor cwd in both panes")
            wait(
                lambda: process_cwds() == original_cwds,
                "renderer process cwd unchanged",
            )

            events = subprocess.Popen(
                ["rex", "events", "-s", session, "--json"],
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )
            received = queue.Queue()

            def collect():
                for line in events.stdout:
                    received.put(json.loads(line))

            threading.Thread(target=collect, daemon=True).start()
            time.sleep(0.3)

            def attention(visual, target):
                # Pane input selects its Neovim window independently of app focus.
                run("rex", "send", "-s", session, "-b", target, "gg0")
                time.sleep(0.2)
                while not received.empty():
                    received.get_nowait()
                lua(
                    "(function() vim.o.belloff=''; vim.o.visualbell="
                    + ("true" if visual else "false")
                    + "; return true end)()"
                )
                run("rex", "send", "-s", session, "-b", target, "h")
                found = []

                def bell_seen():
                    while not received.empty():
                        event = received.get_nowait()
                        # Rex envelopes block events with their full block ID.
                        if "bell" in json.dumps(event):
                            found.append(event)
                    return bool(found)

                wait(bell_seen, "visual bell fallback" if visual else "bell")
                assert all(target in json.dumps(e) for e in found), found

            attention(False, initial)
            attention(True, next(b for b in owned if b != initial))
            # A new renderer must inherit metadata without a new set_title event.
            lua("(function() vim.cmd('split'); return true end)()")
            wait(lambda: len(blocks()) == 3, "late renderer")
            late = next(b for b in blocks() if b not in owned)
            wait(
                lambda: (
                    call(late, "title")["title"] == safe_title
                    and unquote(urlparse(call(late, "pwd").get("pwd", "")).path)
                    == directory
                ),
                "late renderer metadata",
            )
            assert not (ROOT / "SHOULD_NOT_EXIST").exists()
            assert not (Path(directory) / "SHOULD_NOT_EXIST").exists()
            print(
                "PASS: hostile title/cwd, unchanged process cwd, two-pane bell/"
                "visual-bell routing, late-renderer metadata"
            )
        finally:
            if events is not None:
                events.terminate()
                events.communicate(timeout=5)
            run("rex", "kill", session)


if __name__ == "__main__":
    main()
