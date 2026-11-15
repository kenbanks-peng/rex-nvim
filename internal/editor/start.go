package editor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
	"golang.org/x/term"
)

var errNoPanes = errors.New("all editor panes closed")

func Run(ctx context.Context, args []string) error {
	session, block := os.Getenv("REX_SESSION"), os.Getenv("REX_BLOCK")
	if session == "" || block == "" {
		return fmt.Errorf("run rex-nvim inside a Rex terminal block (REX_SESSION and REX_BLOCK are required)")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("stdin must be a terminal")
	}
	width, height, err := term.GetSize(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	// A short, private path also fits macOS's Unix socket path limit.
	dir, err := os.MkdirTemp("/tmp", "rex-nvim-")
	if err != nil {
		return err
	}
	socket := filepath.Join(dir, "ui.sock")
	log, err := os.OpenFile(filepath.Join(dir, "backend.log"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	defer log.Close()
	exe, err := os.Executable()
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	argv := []string{"_serve", socket, session, block, strconv.Itoa(width), strconv.Itoa(height), "--"}
	argv = append(argv, args...)
	cmd := exec.Command(exe, argv...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	failure := func(err error) error {
		data := make([]byte, 32<<10)
		n, _ := log.ReadAt(data, 0)
		if n > 0 {
			return fmt.Errorf("editor backend: %s", strings.TrimSpace(string(data[:n])))
		}
		return err
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			return ctx.Err()
		case <-timeout.C:
			_ = cmd.Process.Kill()
			return failure(fmt.Errorf("editor startup timed out"))
		case err := <-done:
			return failure(fmt.Errorf("editor exited during startup: %v", err))
		case <-ticker.C:
			conn, err := net.DialTimeout("unix", socket, 20*time.Millisecond)
			if err != nil {
				continue
			}
			conn.Close()
			err = terminal.Run(ctx, socket, 0)
			// A renderer can be closed while the backend continues serving other panes.
			select {
			case backendErr := <-done:
				if backendErr != nil {
					return failure(backendErr)
				}
			default:
			}
			return err
		}
	}
}

func (m *manager) disconnected(p *peer) error {
	if p.grid < 0 {
		delete(m.panes, p.grid)
		if p.conn != nil {
			p.conn.Close()
		}
		return nil
	}
	m.layoutShape = ""
	delete(m.panes, p.grid)
	if p.conn != nil {
		p.conn.Close()
	}
	hasTiled := false
	for _, other := range m.panes {
		if other.layer == "" {
			hasTiled = true
			break
		}
	}
	if !hasTiled {
		return errNoPanes
	}
	if p.primary {
		for _, other := range m.panes {
			if other.layer == "" {
				other.primary = true
				m.initial = other.block
				break
			}
		}
	}
	return m.closeWindow(p.grid)
}
