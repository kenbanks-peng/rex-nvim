package screen

import (
	"strings"
	"unicode/utf8"
)

const messageLimit = 128
const messageBytes = 8192

type messageEntry struct {
	content []commandChunk
	id      any
}
type messageState struct {
	batchShows          int
	batchEmpty          bool
	visible, history    []messageEntry
	mode, cmd, ruler    []commandChunk
	historyOpen         bool
	offset, page, owner int
}

func (s *State) SetExternalMessages(enabled bool) {
	// ext_messages implicitly enables ext_cmdline: never request it without
	// that renderer, including when the explicit grid fallback is selected.
	s.externalMessages = enabled && s.externalCmdline
	if !s.externalMessages {
		s.messages = messageState{}
	}
}

// Limit bytes and chunk count together; cloning avoids retaining RPC buffers.
func messageChunks(raw any) []commandChunk {
	chunks := commandChunks(raw)
	out := make([]commandChunk, 0, min(len(chunks), messageBytes))
	remaining := messageBytes
	for _, c := range chunks {
		if remaining == 0 {
			break
		}
		n := min(len(c.text), remaining)
		for n > 0 && !utf8.ValidString(c.text[:n]) {
			n--
		}
		if n == 0 {
			continue
		}
		out = append(out, commandChunk{strings.Clone(c.text[:n]), c.hl})
		remaining -= n
	}
	return out
}
func boundMessages(entries []messageEntry) []messageEntry {
	if len(entries) > messageLimit {
		entries = append([]messageEntry(nil), entries[len(entries)-messageLimit:]...)
	}
	return entries
}
func joinMessage(a, b []commandChunk) []commandChunk {
	raw := make([]any, 0, len(a)+len(b))
	for _, c := range append(append([]commandChunk(nil), a...), b...) {
		raw = append(raw, []any{c.hl, c.text})
	}
	return messageChunks(raw)
}
func messageID(raw any) any {
	switch id := raw.(type) {
	case string:
		if len(id) <= 128 {
			return strings.Clone(id)
		}
	case int, int64, uint64, uint, int32, uint32:
		// Normalize the integer widths used by msgpack.
		return integer(raw)
	}
	return nil
}
func (s *State) messageEvent(name string, a []any) {
	if !s.externalMessages {
		return
	}
	at := func(i int) any {
		if i < len(a) {
			return a[i]
		}
		return nil
	}
	m := &s.messages
	switch name {
	case "msg_show":
		m.batchShows++
		kind, _ := at(0).(string)
		if kind == "return_prompt" && m.historyOpen {
			// API 13 follows history with native hit-enter. Do not cover that
			// prompt with the local viewer or consume keys meant for Neovim.
			m.visible = append([]messageEntry(nil), m.history...)
			m.historyOpen = false
		}
		// The history flag is informational: Neovim owns its log, and sends
		// authoritative msg_history_show snapshots when requested.
		entry := messageEntry{content: messageChunks(at(1)), id: messageID(at(5))}
		if kind == "empty" && len(entry.content) == 0 {
			m.batchEmpty = true
			return
		}
		index := -1
		if entry.id != nil {
			for i, old := range m.visible {
				if old.id == entry.id {
					index = i
					break
				}
			}
		}
		if index < 0 && len(m.visible) > 0 && (boolean(at(2)) || boolean(at(4))) {
			index = len(m.visible) - 1
		}
		if index >= 0 {
			if boolean(at(4)) {
				entry.content = joinMessage(m.visible[index].content, entry.content)
			}
			m.visible[index] = entry
		} else {
			m.visible = boundMessages(append(m.visible, entry))
		}
	case "msg_history_clear":
		m.history, m.historyOpen, m.offset = nil, false, 0
	case "msg_clear":
		m.visible = nil
	case "msg_showmode":
		m.mode = messageChunks(at(0))
	case "msg_showcmd":
		m.cmd = messageChunks(at(0))
	case "msg_ruler":
		m.ruler = messageChunks(at(0))
	case "msg_history_show":
		// Neovim owns history. This is a snapshot, never an addition to our own log.
		m.history = nil
		for _, raw := range sequence(at(0)) {
			fields := sequence(raw)
			if len(fields) < 2 {
				continue
			}
			content := messageChunks(fields[1])
			if len(fields) > 2 && boolean(fields[2]) && len(m.history) > 0 {
				i := len(m.history) - 1
				m.history[i].content = joinMessage(m.history[i].content, content)
			} else {
				m.history = boundMessages(append(m.history, messageEntry{content: content}))
			}
		}
		m.historyOpen, m.offset, m.owner = true, 0, s.commandOwner()
	}
}

// MessageInput clears transient messages and owns the local history viewer.
// Call only when Neovim is not blocking. Native confirmation and
// hit-enter prompts must receive every key through nvim_input.
func (s *State) MessageInput(keys string) bool {
	if s == nil {
		return false
	}
	m := &s.messages
	if s.promptInput || s.CmdlineActive() {
		return false
	}
	if !m.historyOpen {
		m.visible = nil
		return false
	}
	switch keys {
	case "<PageUp>", "<C-b>":
		m.offset += max(1, m.page)
	case "<PageDown>", "<C-f>":
		m.offset = max(0, m.offset-max(1, m.page))
	default:
		m.historyOpen = false
		return keys == "<CR>" || keys == "<Esc>" || keys == " " || keys == "<C-c>"
	}
	return true
}
func (s *State) drawMessages(frame []pixel, id, width, height int) {
	if !s.externalMessages || width < 1 || height < 1 {
		return
	}
	m := &s.messages
	owner := s.commandOwner()
	if m.historyOpen {
		owner = m.owner
		if v := s.views[owner]; v == nil || v.Hidden {
			owner = s.commandOwner()
		}
	}
	if owner != id {
		return
	}
	entries := m.visible
	if m.historyOpen {
		entries = m.history
	}
	var rows [][]pixel
	for _, e := range entries {
		p, _ := commandPixels(e.content, 0)
		rows = append(rows, wrapCommand(p, width, -1).rows...)
	}
	var status []commandChunk
	for _, chunks := range [][]commandChunk{m.mode, m.cmd, m.ruler} {
		if len(chunks) == 0 {
			continue
		}
		if len(status) > 0 {
			status = append(status, commandChunk{"  ", 0})
		}
		status = append(status, chunks...)
	}
	if len(status) > 0 && !m.historyOpen {
		p, _ := commandPixels(status, 0)
		rows = append(rows, wrapCommand(p, width, -1).rows...)
	}
	if m.historyOpen {
		m.page = max(1, height-1)
		m.offset = min(m.offset, max(0, len(rows)-m.page))
		end := max(0, len(rows)-m.offset)
		rows = rows[max(0, end-m.page):end]
		p, _ := commandPixels([]commandChunk{{"History: PgUp/PgDn; Enter/Esc dismiss", 0}}, 0)
		rows = append(rows, wrapCommand(p, width, -1).rows[0])
	}
	rows = rows[max(0, len(rows)-height):]
	for y, row := range rows {
		copy(frame[(height-len(rows)+y)*width:], row)
	}
}

func (s *State) finishMessages() {
	m := &s.messages
	if m.batchEmpty && m.batchShows == 1 {
		m.visible = nil
	}
	m.batchShows, m.batchEmpty = 0, false
}
