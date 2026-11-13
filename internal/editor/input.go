package editor

import (
	"strings"
	"time"

	"github.com/neovim/go-client/nvim"
)

type inputRoute struct{ grid, window int }

func (m *manager) routePeer(p *peer) {
	for _, v := range m.views() {
		if v.Grid == p.grid {
			p.route.Store(&inputRoute{p.grid, v.Window})
			return
		}
	}
}

// Non-fast focus and paste RPCs may run user callbacks that prompt for input.
// Keep pumping fast input while they wait, without sending the triggering key
// to the wrong window.
func (m *manager) sendKeys() {
	var pending []incoming
	for {
		var e incoming
		if len(pending) > 0 {
			e, pending = pending[0], pending[1:]
		} else {
			select {
			case <-m.ctx.Done():
				return
			case e = <-m.keys:
			}
		}
		route := e.p.route.Load()
		if route == nil {
			continue
		}
		if m.activeGrid.Load() != int64(route.grid) && e.msg.Type != "mouse" {
			previous := m.activeGrid.Swap(int64(route.grid))
			queued, err := m.pumpOperation(func() error { return m.nv.SetCurrentWindow(nvim.Window(route.window)) })
			if err == nil {
				m.activeGrid.Store(int64(route.grid))
				pending = append(append([]incoming{e}, queued...), pending...)
			} else {
				m.activeGrid.CompareAndSwap(int64(route.grid), previous)
				pending = append(queued, pending...)
			}
			continue
		}
		if e.msg.Type == "paste" {
			queued, _ := m.pumpOperation(func() error { _, err := m.nv.Paste(e.msg.Keys, true, -1); return err })
			pending = append(queued, pending...)
			continue
		}
		if m.deliverFast(e) != nil {
			return
		}
	}
}
func (m *manager) pumpOperation(operation func() error) ([]incoming, error) {
	done := make(chan error, 1)
	started := time.Now()
	go func() { done <- operation() }()
	var queued []incoming
	for {
		select {
		case <-m.ctx.Done():
			return queued, m.ctx.Err()
		case err := <-done:
			return queued, err
		case e := <-m.keys:
			select {
			case err := <-done:
				return append(queued, e), err
			default:
			}
			// Preserve an already-typed burst while a local focus RPC completes.
			// Later input must still reach callbacks waiting in getchar()/input();
			// nvim_get_mode().blocking does not identify those callback prompts.
			if time.Since(started) < 25*time.Millisecond {
				queued = append(queued, e)
			} else {
				if err := m.deliverFast(e); err != nil {
					return queued, err
				}
			}
		}
	}
}
func (m *manager) deliverFast(e incoming) error {
	route := e.p.route.Load()
	if route == nil {
		return nil
	}
	msg := e.msg
	switch msg.Type {
	case "input":
		return m.typeKeys(msg.Keys)
	case "paste":
		text := strings.ReplaceAll(msg.Keys, "<", "<LT>")
		text = strings.ReplaceAll(text, "\n", "<CR>")
		return m.typeKeys(text)
	case "mouse":
		return m.nv.InputMouse(msg.Button, msg.Action, msg.Modifier, route.grid, msg.Row, msg.Col)
	}
	return nil
}
func (m *manager) typeKeys(keys string) error {
	for len(keys) > 0 {
		n, err := m.nv.Input(keys)
		if err != nil {
			return err
		}
		keys = keys[n:]
		if n == 0 {
			select {
			case <-m.ctx.Done():
				return m.ctx.Err()
			case <-time.After(2 * time.Millisecond):
			}
		}
	}
	return nil
}
