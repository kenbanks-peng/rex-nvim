package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/kenbanks-peng/rex-nvim/internal/editor"
	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
)

var version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()
	args := os.Args[1:]
	var err error
	switch {
	case len(args) > 0 && (args[0] == "--help" || args[0] == "help"):
		fmt.Println("Usage: rex-nvim [Neovim arguments...]\n\nRun one headless Neovim with each window in a Rex block.\nRequires Rex and Neovim 0.11+.\n\n  rex-nvim file.go\n  rex-nvim --clean file.go\n  rex-nvim --version")
		return
	case len(args) > 0 && args[0] == "--version":
		fmt.Println("rex-nvim", version)
		return
	case len(args) > 0 && args[0] == "_serve":
		if len(args) < 7 || args[6] != "--" {
			err = fmt.Errorf("invalid internal backend invocation")
			break
		}
		var width, height int
		width, err = strconv.Atoi(args[4])
		if err != nil {
			break
		}
		height, err = strconv.Atoi(args[5])
		if err != nil {
			break
		}
		if width < 1 || height < 1 {
			err = fmt.Errorf("invalid terminal dimensions")
			break
		}
		err = editor.Serve(ctx, args[1], args[2], args[3], width, height, args[7:])
	case len(args) > 0 && args[0] == "_pane":
		if len(args) != 3 {
			err = fmt.Errorf("invalid internal pane invocation")
			break
		}
		var grid int
		grid, err = strconv.Atoi(args[2])
		if err == nil {
			err = terminal.Run(ctx, args[1], grid)
		}
	default:
		err = editor.Run(ctx, args)
	}
	if err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "rex-nvim:", err)
		os.Exit(1)
	}
}
