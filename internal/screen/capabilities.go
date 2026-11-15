package screen

import "strings"

// Capability describes a UI protocol family, not a promise of full API coverage.
// Since is the Neovim API level (not a release number). See docs/ui-capabilities.md.
type Capability struct {
	Family, Status, Since, Fallback, Rationale string
	Events, Methods                            string
}

// Capabilities returns an independent inventory. Optional means deliberately
// disabled until a complete external widget replaces Neovim's grid rendering.
func Capabilities() []Capability {
	return []Capability{
		{"linegrid", "supported", "grid events:5; grid_destroy:6; flush:3", "Required; no legacy renderer or host reflow.", "Cell updates and flush boundaries; wrap is advisory because frames explicitly position every row.", "grid_resize grid_clear grid_cursor_goto grid_line grid_scroll grid_destroy flush", ""},
		{"highlights", "partial", "default_colors_set:4; hl_attr_define:5", "RGB styles and five underline variants; missing RGB colors inherit live defaults, not cterm overrides.", "Opaque blend; normal font; URLs remain text. Rex supports SGR underline styles/colors, faint, blink, conceal and overline.", "default_colors_set hl_attr_define", ""},
		{"multigrid", "partial", "6", "Grid composition uses compindex/resolved screen coordinates when supplied, otherwise zindex/anchors.", "Rex pane placement is approximate; managed float layers follow Neovim order without focus changes. Native messages use a full-window layer; the optional scroll separator is omitted.", "win_pos win_float_pos win_hide win_close msg_set_pos", ""},
		{"cursor", "partial", "3", "Default steady block; unknown cursor colors reset to the host default.", "Native shape/blink and attr_id fill color; terminal thickness/timings/glyph foreground replace exact values. attr_id_lm is not selected.", "mode_info_set mode_change", ""},
		{"viewport", "unsupported", "win_viewport:7", "Apply grid_scroll/grid_line immediately; Neovim owns buffer-to-screen mapping and buffer-relative float positions.", "No host scrollbar or smooth-scroll animation consumes buffer ranges/scroll_delta; retaining them would duplicate unused state. Never apply scroll_delta as a second grid scroll.", "win_viewport", ""},
		{"viewport-margins", "partial", "12", "Tiled winbar rows use independent floating chrome regions; external windows retain their complete grids.", "Top margins identify winbars without changing native grid coordinates. Other margins remain native grid content.", "win_viewport_margins", ""},
		{"extmark-position", "unsupported", "10", "Neovim renders highlights, signs, virtual text/lines and positions buffer-relative floats.", "No Rex-owned extmark overlay consumes positions; do not create ui_watched marks or request notifications. Plugin-requested notifications are ignored, not retained across grid/window reuse.", "win_extmark", ""},
		{"external-windows", "partial", "6", "External windows use independent Rex layers; their renderer sizes drive per-grid resize.", "Initial placement is centered, then Rex owns geometry. External-anchored floats/popups compose inside that renderer; external windows never join tiled layout.", "win_external_pos", ""},
		{"legacy-drawing", "unsupported", "3", "ext_linegrid replaces these events.", "A second drawing implementation is unnecessary; linegrid must remain enabled.", "update_fg update_bg update_sp resize clear eol_clear cursor_goto highlight_set put set_scroll_region scroll", ""},
		{"semantic-highlights", "optional", "6", "hl_attr_define RGB attributes remain authoritative.", "ext_hlstate is disabled; semantic group provenance is not needed for terminal cells.", "hl_group_set", ""},
		{"popupmenu", "partial", "events:3; nvim_ui_pum_set_height:6; nvim_ui_pum_set_bounds:7", "The backend uses native Neovim completion grids.", "Pane-local anchored completion overlay, scrolling selection and cancellation; clipped geometry reported to Neovim. Labels are ASCII-escaped; info remains available to plugins, not a separate documentation widget.", "popupmenu_show popupmenu_hide popupmenu_select", "nvim_ui_pum_set_height nvim_ui_pum_set_bounds"},
		{"cmdline", "supported", "3; prompt hl_id/abort:14", "The backend uses native command-line and block grids.", "Pane-local highlighted nested prompts and blocks, byte-to-grapheme cell positions, wrapping and cursor-follow clipping; input and cancellation remain Neovim-owned.", "cmdline_show cmdline_pos cmdline_special_char cmdline_hide cmdline_block_show cmdline_block_append cmdline_block_hide", ""},
		{"messages", "partial", "6; history_clear:10 (removed in 14); append:13; id/trigger:14", "The backend uses native message grids and pagination.", "Pane-local highlighted messages/status, replacement/append and ID updates; authoritative history is bounded to 128 entries of 8192 bytes, paged locally. Native prompts own input. No timed notifications or independent host widget.", "msg_show msg_clear msg_showcmd msg_showmode msg_ruler msg_history_show msg_history_clear", ""},
		{"tabline", "partial", "tabs:3; buffers:7", "The backend uses native full-width tabline cells and root mouse selection.", "Pane-local Nvim tabs/Buffers rows select handles through Neovim APIs; previous/next controls reach overflow. showtabline controls visibility; native commands own create/rename/close. Small panes omit navigation, never content.", "tabline_update", ""},
		{"wildmenu", "optional", "3", "Neovim draws wildmenu in grids.", "Legacy ext_wildmenu is disabled; no external widget.", "wildmenu_show wildmenu_select wildmenu_hide", ""},
		{"options", "partial", "4; nvim_ui_set_option:1", "rgb/ext_linegrid/ext_multigrid; native chrome uses full-window floating layers. External widget renderers are not attached by the backend.", "option_set maps mousemoveevent to host motion reporting; grid widths/colors/widgets remain Neovim-owned. Fonts, spacing and pointer styling use host defaults. Dynamic UI protocol toggles are not exposed.", "option_set", "nvim_ui_set_option"},
		{"host-decoration", "partial", "set_title/set_icon:3; chdir:12", "OSC 2 titles and OSC 7 editor-directory reports reach every owned pane; icon is retained without changing the host icon.", "Strip title controls, URI-encode absolute paths, reject relative/NUL paths. Editor cwd is metadata, never process chdir; no Rex pane icon API.", "set_title set_icon chdir", ""},
		{"host-feedback", "partial", "3", "Grid contents remain visible; input continues while the cursor is hidden.", "Busy start/stop suppress/restore the cursor; mouse on/off controls reporting in every pane. Bell and visual bell map to pane-local BEL (host attention policy), coalesced while pending; no independent Rex visual flash API.", "bell visual_bell busy_start busy_stop mouse_on mouse_off", ""},
		{"host-actions", "unsupported", "suspend/update_menu:3; screenshot:7; error_exit:12", "Menus remain usable through Neovim commands; EOF controls backend shutdown.", "Neovim suspend is deliberately declined: no single foreground job owns all panes. Renderer SIGTSTP/SIGCONT restores terminal modes and retains its connection; the backend and sibling renderers continue. No native menu, screenshot service or special exit-code propagation.", "suspend update_menu screenshot error_exit", ""},
		{"server-handoff", "unsupported", "14 (Neovim 0.12+)", "Start a new rex-nvim for another server; no transparent reconnect.", "connect/restart detach or exit the owned child. Events are diagnosed but never interpreted as addresses to dial.", "connect restart", ""},
		{"terminal-passthrough", "unsupported", "nvim_ui_term_event:12; ui_send/nvim_ui_send:14", "Use grid-rendered text; stdout_tty is not advertised.", "No arbitrary terminal controls, graphics or terminal responses are forwarded across shared panes.", "ui_send", "nvim_ui_term_event nvim_ui_send"},
		{"attachment", "partial", "1", "Backend shutdown attempts bounded nvim_ui_detach, then closes RPC and its owned child even if detach fails.", "Renderer suspension retains the UI; renderer recovery reuses the same editor. No public detach/reattach or server-handoff API. RPC close is the cleanup fallback after child failure.", "", "nvim_ui_attach nvim_ui_detach"},
		{"resize", "partial", "nvim_ui_try_resize:1; nvim_ui_try_resize_grid:6", "Resize tiled root UI through layout; resize genuinely external windows with nvim_ui_try_resize_grid.", "Ordinary float grids remain Neovim-owned and clip to their renderer; external grids follow terminal dimensions.", "", "nvim_ui_try_resize nvim_ui_try_resize_grid"},
		{"host-focus", "partial", "11", "Rex pane focus selects Neovim windows independently of UI focus.", "Terminal gained/lost reports and Rex active-pane transitions drive deduplicated nvim_ui_set_focus. Pane losses coalesce for 75ms; unchanged session snapshots cannot infer OS activation without terminal reports.", "", "nvim_ui_set_focus"},
	}
}

func eventCapability(name string) (Capability, bool) {
	for _, c := range Capabilities() {
		for _, event := range strings.Fields(c.Events) {
			if event == name {
				return c, true
			}
		}
	}
	return Capability{}, false
}

// UIOptions returns fresh attachment options. Optional presentations are enabled
// only when the caller supplies their renderers: tabline, popupmenu, cmdline, messages.
func UIOptions(presentation ...bool) map[string]any {
	options := map[string]any{"rgb": true, "ext_linegrid": true, "ext_multigrid": true}
	if len(presentation) > 0 && presentation[0] {
		options["ext_tabline"] = true
	}
	if len(presentation) > 1 && presentation[1] {
		options["ext_popupmenu"] = true
	}
	if len(presentation) > 2 && presentation[2] {
		options["ext_cmdline"] = true
	}
	if len(presentation) > 3 && presentation[3] && len(presentation) > 2 && presentation[2] {
		options["ext_messages"] = true
	}
	return options
}
