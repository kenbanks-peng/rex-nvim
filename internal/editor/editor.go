package editor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
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
	ready                    chan struct{}
	route                    atomic.Pointer[inputRoute]
	grid, width, height      int
	block, layer             string
	conn                     net.Conn
	frames                   chan string
	host                     chan terminal.Message
	control                  chan terminal.Message
	suspended                bool
	hostTitle, hostDirectory *string
	last                     string
	primary                  bool
	broken                   bool
	bounds                   [4]float64
}
type incoming struct {
	p   *peer
	msg terminal.Message
	err error
}
type manager struct {
	paint                       chan struct{}
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
	uiFocusKnown, uiFocused     bool
	focusBlock, rexFocusBlock   string
	blurAt                      time.Time
	layoutShape                 string
	fittedLayout                *layoutNode
	popupGeometry               screen.PopupGeometry
	popupKnown                  bool
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
	m.state.OnIgnoredEvent = func(name string, suppressed bool) {
		if suppressed {
			log.Print("Neovim redraw: further ignored-event diagnostics suppressed")
			return
		}
		log.Printf("Neovim redraw: ignoring unhandled event %q (see UI capability inventory)", name)
	}
	childArgs := append([]string{"--embed", "--headless", "--cmd", "let g:rex_nvim = v:true"}, args...)
	nv, err := nvim.NewChildProcess(nvim.ChildProcessContext(ctx), nvim.ChildProcessArgs(childArgs...), nvim.ChildProcessServe(false))
	if err != nil {
		return err
	}
	m.nv = nv
	defer nv.Close()
	if err = nv.RegisterHandler("redraw", func(updates ...[]any) {
		m.mu.Lock()
		flushed := m.state.Apply(updates...) || m.state.HostPending()
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
	externalTabline := os.Getenv("REX_NVIM_TABLINE") != "grid"
	m.state.SetExternalTabline(externalTabline)
	externalPopupmenu := os.Getenv("REX_NVIM_POPUPMENU") != "grid"
	m.state.SetExternalPopupmenu(externalPopupmenu)
	externalCmdline := os.Getenv("REX_NVIM_CMDLINE") != "grid"
	m.state.SetExternalCmdline(externalCmdline)
	externalMessages := externalCmdline && os.Getenv("REX_NVIM_MESSAGES") != "grid"
	m.state.SetExternalMessages(externalMessages)
	if err = nv.AttachUI(w, h, screen.UIOptions(externalTabline, externalPopupmenu, externalCmdline, externalMessages)); err != nil {
		return err
	}
	if externalMessages {
		if err = nv.ExecLua("vim.g.rex_nvim_cmdheight=vim.o.cmdheight", nil); err != nil {
			return err
		}
	}
	m.paint = make(chan struct{}, 1)
	m.keys = make(chan incoming, 256)
	go m.sendKeys()
	go m.accept(listener)
	defer func() {
		// Detach while RPC is live. On failure/hang, cancellation and Close
		// release the UI and owned child; renderer sockets are always closed.
		if err := detachUI(nv.DetachUI, 250*time.Millisecond); err != nil {
			log.Printf("Neovim UI detach: %v", err)
		}
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
	focusTicker := time.NewTicker(25 * time.Millisecond)
	defer focusTicker.Stop()
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
		case <-m.paint:
			m.draw()
		case <-m.wake:
			if err = m.reconcile(); err != nil && !rex.IsRecoverable(err) {
				return err
			}
			m.draw()
		case <-focusTicker.C:
			if err = m.flushBlur(time.Now()); err != nil && !rex.IsRecoverable(err) {
				return err
			}
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
			p := &peer{conn: conn, frames: make(chan string, 1), host: make(chan terminal.Message, 1), control: make(chan terminal.Message, 1), ready: make(chan struct{})}
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
				host := p.host
				for {
					select {
					case frame := <-frames:
						conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
						if e.Encode(terminal.Message{Type: "frame", Frame: frame}) != nil {
							conn.Close()
							return
						}
					case msg := <-p.control:
						if msg.Type == "suspend" {
							frames, host = nil, nil
							conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
							if e.Encode(terminal.Message{Type: "suspended"}) != nil {
								conn.Close()
								return
							}
						} else {
							frames, host = p.frames, p.host
						}
					case msg := <-host:
						conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
						if e.Encode(msg) != nil {
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
				if !v.Float && !v.External && !v.Hidden {
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
		p.bounds = target.bounds
		m.panes[target.grid] = p
		m.routePeer(p)
		if p.ready != nil {
			close(p.ready)
		}
		m.draw()
		return m.fitRenderer(p)
	}
	target := m.panes[p.grid]
	if target != p {
		return nil
	}
	if e.err != nil {
		return m.transportLost(p)
	}
	switch e.msg.Type {
	case "suspend", "resume":
		p.suspended = e.msg.Type == "suspend"
		select {
		case p.control <- e.msg:
		case <-m.ctx.Done():
			return m.ctx.Err()
		}
		if !p.suspended {
			p.last = ""
			m.draw()
		}
		return nil
	case "resize":
		p.width, p.height = e.msg.Width, e.msg.Height
		m.routePeer(p)
		if m.isExternal(p.grid) {
			return m.fitRenderer(p)
		}
		if p.layer != "" {
			m.draw()
			return nil
		}
		return m.fit()
	case "focus":
		m.focusBlock, m.rexFocusBlock = p.block, p.block
		m.blurAt = time.Time{}
		if err := m.setUIFocus(true); err != nil {
			return err
		}
		return m.focus(p.grid)
	case "blur":
		if m.focusBlock == "" || m.focusBlock == p.block {
			m.blurAt = time.Now().Add(75 * time.Millisecond)
		}
	}
	return nil
}
func (m *manager) views() []screen.View { m.mu.Lock(); defer m.mu.Unlock(); return m.state.Views() }
func (m *manager) focus(grid int) error {
	m.mu.Lock()
	commandActive := m.state.CommandInputActive()
	m.mu.Unlock()
	if commandActive || m.blockingInput() {
		return nil
	}
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
	m.drawFrames()
	m.reportPopupGeometry()
}

func (m *manager) drawFrames() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.drawHost()
	external := map[int]bool{}
	for grid := range m.panes {
		external[grid] = true
	}
	m.state.SetExternalGrids(external)
	for grid, p := range m.panes {
		if p.suspended || p.frames == nil || p.width < 1 || p.height < 1 {
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
