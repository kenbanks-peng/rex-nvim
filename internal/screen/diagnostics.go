package screen

import "strings"

// Keep the active dispatcher explicit: inventory tests require every handler to
// be classified and every supported event to have a handler.
var handledEvents = map[string]bool{
	"msg_show": true, "msg_clear": true, "msg_showmode": true, "msg_showcmd": true, "msg_ruler": true, "msg_history_show": true, "msg_history_clear": true,
	"cmdline_show": true, "cmdline_pos": true, "cmdline_special_char": true, "cmdline_hide": true, "cmdline_block_show": true, "cmdline_block_append": true, "cmdline_block_hide": true,
	"popupmenu_show": true, "popupmenu_select": true, "popupmenu_hide": true,
	"flush": true, "tabline_update": true,
	"set_title": true, "set_icon": true, "chdir": true, "bell": true, "visual_bell": true,
	"mouse_on": true, "mouse_off": true, "busy_start": true, "busy_stop": true, "option_set": true,
	"grid_resize": true, "grid_clear": true, "grid_destroy": true,
	"grid_line": true, "grid_scroll": true, "grid_cursor_goto": true,
	"win_external_pos": true, "win_pos": true, "win_float_pos": true, "win_hide": true,
	"win_close": true, "msg_set_pos": true, "win_viewport_margins": true,
	"mode_info_set": true, "mode_change": true,
	"hl_attr_define": true, "default_colors_set": true,
}

func (s *State) ignoreEvent(name string) {
	if len(name) > 128 {
		name = name[:128]
	}
	if s.ignoredEvents[name] || s.ignoredLimit {
		return
	}
	if s.ignoredEvents == nil {
		s.ignoredEvents = make(map[string]bool)
	}
	if len(s.ignoredEvents) == 32 {
		s.ignoredLimit = true
		if s.OnIgnoredEvent != nil {
			s.OnIgnoredEvent("", true)
		}
		return
	}
	// Do not retain an arbitrarily large backing string after truncation.
	name = strings.Clone(name)
	s.ignoredEvents[name] = true
	if s.OnIgnoredEvent != nil {
		s.OnIgnoredEvent(name, false)
	}
}
