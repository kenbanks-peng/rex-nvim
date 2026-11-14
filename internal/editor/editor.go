package editor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kenbanks-peng/rex-nvim/internal/rex"
	"github.com/kenbanks-peng/rex-nvim/internal/screen"
	"github.com/kenbanks-peng/rex-nvim/internal/terminal"
	"github.com/neovim/go-client/nvim"
)

type peer struct {
	ready               chan struct{}
	route               atomic.Pointer[inputRoute]
	grid, width, height int
	block, layer        string
	conn                net.Conn
	frames              chan string
	last                string
	primary             bool
	broken              bool
	bounds              [4]float64
	geometry            [4]int
	floatRoot           [2]int
	floatSize           [2]int
	floatResize         [2]int
}
type incoming struct {
	p   *peer
	msg terminal.Message
	err error
}
type manager struct {
	ctx                         context.Context
	nv                          *nvim.Nvim
	rex                         *rex.Client
	state                       *screen.State
	mu                          sync.Mutex
	wake                        chan struct{}
	input                       chan incoming
	keys                        chan incoming
	activeGrid                  atomic.Int64
	panes                       map[int]*peer
	socket, executable, initial string
	rootW, rootH                int
	lastFocus                   int
	layoutShape                 string
	fittedLayout                *layoutNode
}

// Serve owns the shared editor independently of every renderer's terminal.
func Serve(parent context.Context, socket, session, block string, w, h int, args []string) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	if err := validateBackendPath(socket); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(socket), ".backend"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	lock.Close()
	defer os.RemoveAll(filepath.Dir(socket))
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	m := &manager{ctx: ctx, rex: rex.New(session), state: screen.New(), wake: make(chan struct{}, 1), input: make(chan incoming, 256), panes: map[int]*peer{}, socket: socket, executable: exe, initial: block, rootW: w, rootH: h}
	childArgs := append([]string{"--embed", "--headless", "--cmd", "let g:rex_nvim = v:true"}, args...)
	nv, err := nvim.NewChildProcess(nvim.ChildProcessContext(ctx), nvim.ChildProcessArgs(childArgs...), nvim.ChildProcessServe(false))
	if err != nil {
		return err
	}
	m.nv = nv
	defer nv.Close()
	if err = nv.RegisterHandler("redraw", func(updates ...[]any) {
		m.mu.Lock()
		flushed := m.state.Apply(updates...)
		m.mu.Unlock()
		if flushed {
			select {
			case m.wake <- struct{}{}:
			default:
			}
		}
	}); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- nv.Serve() }()
	if err = nv.ExecLua(configure, nil); err != nil {
		return err
	}
	if err = nv.AttachUI(w, h, map[string]any{"rgb": true, "ext_multigrid": true, "ext_linegrid": true}); err != nil {
		return err
	}
	m.keys = make(chan incoming, 256)
	go m.sendKeys()
	go m.accept(listener)
	defer func() {
		cancel()
		listener.Close()
		for _, p := range m.panes {
			if p.conn != nil {
				p.conn.Close()
			}
		}
		cleanup, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		for _, p := range m.panes {
			if p.primary {
				continue
			}
			if p.layer != "" {
				_ = m.rex.CloseLayer(cleanup, p.layer)
			} else {
				_ = m.rex.Close(cleanup, p.block)
			}
		}
	}()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-done:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case event := <-m.input:
			if err = m.handle(event); err != nil && !rex.IsRecoverable(err) {
				if errors.Is(err, errNoPanes) {
					return nil
				}
				return err
			}
		case <-m.wake:
			if err = m.reconcile(); err != nil && !rex.IsRecoverable(err) {
				return err
			}
			m.draw()
		case <-ticker.C:
			if err = m.syncRex(); err != nil && !rex.IsRecoverable(err) {
				if errors.Is(err, errNoPanes) {
					return nil
				}
				return err
			}
		}
	}
}

func (m *manager) accept(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		go func() {
			p := &peer{conn: conn, frames: make(chan string, 1), ready: make(chan struct{})}
			defer conn.Close()
			d := json.NewDecoder(io.LimitReader(conn, 1<<40))
			var hello terminal.Message
			if err := d.Decode(&hello); err != nil || hello.Type != "hello" {
				return
			}
			p.grid = hello.Grid
			select {
			case m.input <- incoming{p: p, msg: hello}:
			case <-m.ctx.Done():
				return
			}
			select {
			case <-p.ready:
			case <-m.ctx.Done():
				return
			}
			connectionDone := make(chan struct{})
			defer close(connectionDone)
			frames := p.frames
			go func() {
				e := json.NewEncoder(conn)
				for {
					select {
					case frame := <-frames:
						conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
						if e.Encode(terminal.Message{Type: "frame", Frame: frame}) != nil {
							conn.Close()
							return
						}
					case <-m.ctx.Done():
						return
					case <-connectionDone:
						return
					}
				}
			}()
			for {
				var msg terminal.Message
				err := d.Decode(&msg)
				destination := m.input
				if err == nil && (msg.Type == "input" || msg.Type == "paste" || msg.Type == "mouse") {
					destination = m.keys
				}
				select {
				case destination <- incoming{p: p, msg: msg, err: err}:
				case <-m.ctx.Done():
					return
				}
				if err != nil {
					return
				}
			}
		}()
	}
}
func (m *manager) handle(e incoming) error {
	p := e.p
	if e.msg.Type == "hello" {
		target := m.panes[p.grid]
		if p.grid == 0 {
			for _, v := range m.views() {
				if !v.Float && !v.Hidden {
					target = m.panes[v.Grid]
					break
				}
			}
		}
		if target == nil { // redraw and helper startup can race
			if p.grid != 0 {
				if p.ready != nil {
					close(p.ready)
				}
				if p.conn != nil {
					p.conn.Close()
				}
				return nil
			}
			target = &peer{primary: true, block: m.initial}
			m.panes[0] = target
		}
		if e.msg.Block != "" && e.msg.Block != target.block {
			if p.ready != nil {
				close(p.ready)
			}
			if p.conn != nil {
				p.conn.Close()
			}
			return nil
		}
		if target.conn != nil && target.conn != p.conn {
			target.conn.Close()
		}
		p.grid = target.grid
		p.width, p.height = e.msg.Width, e.msg.Height
		// Keep the read loop's peer as the stable identity.
		p.block, p.layer, p.primary = target.block, target.layer, target.primary
		p.bounds, p.geometry, p.floatRoot = target.bounds, target.geometry, target.floatRoot
		p.floatSize, p.floatResize = target.floatSize, target.floatResize
		m.panes[target.grid] = p
		m.routePeer(p)
		if p.ready != nil {
			close(p.ready)
		}
		if p.layer != "" {
			if err := m.fitFloat(p); err != nil {
				return err
			}
		}
		m.draw()
		return m.fit()
	}
	target := m.panes[p.grid]
	if target != p {
		return nil
	}
	if e.err != nil {
		return m.transportLost(p)
	}
	switch e.msg.Type {
	case "resize":
		p.width, p.height = e.msg.Width, e.msg.Height
		if p.layer != "" {
			return m.fitFloat(p)
		}
		return m.fit()
	case "focus":
		return m.focus(p.grid)
	}
	return nil
}
func (m *manager) views() []screen.View { m.mu.Lock(); defer m.mu.Unlock(); return m.state.Views() }
func (m *manager) focus(grid int) error {
	if int64(grid) == m.activeGrid.Load() {
		return nil
	}
	for _, v := range m.views() {
		if v.Grid == grid {
			if v.Float && !v.Focusable {
				m.mu.Lock()
				cursor := m.state.CursorGrid()
				m.mu.Unlock()
				if cursor != grid {
					if active := m.panes[cursor]; active != nil {
						return m.rex.Focus(m.ctx, active.block)
					}
					if active := m.panes[m.lastFocus]; active != nil {
						return m.rex.Focus(m.ctx, active.block)
					}
					return nil
				}
			}
			previous := m.activeGrid.Swap(int64(grid))
			if err := m.nv.SetCurrentWindow(nvim.Window(v.Window)); err != nil {
				m.activeGrid.CompareAndSwap(int64(grid), previous)
				return err
			}
			m.lastFocus = grid
			m.activeGrid.Store(int64(grid))
			return nil
		}
	}
	return nil
}
func (m *manager) closeWindow(grid int) error {
	for _, v := range m.views() {
		if v.Grid == grid {
			// Never force-close a modified buffer. A rejected close recreates its Rex block.
			_ = m.nv.CloseWindow(nvim.Window(v.Window), false)
			return nil
		}
	}
	return nil
}
func (m *manager) draw() {
	m.mu.Lock()
	defer m.mu.Unlock()
	external := map[int]bool{}
	for grid := range m.panes {
		external[grid] = true
	}
	m.state.SetExternalGrids(external)
	for grid, p := range m.panes {
		if p.frames == nil || p.width < 1 || p.height < 1 {
			continue
		}
		frame := m.state.Render(grid, p.width, p.height)
		if frame == p.last {
			continue
		}
		p.last = frame
		select {
		case p.frames <- frame:
		default:
			select {
			case <-p.frames:
			default:
			}
			select {
			case p.frames <- frame:
			default:
			}
		}
	}
}
