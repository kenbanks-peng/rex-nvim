local api = vim.api
local open_win, set_config = api.nvim_open_win, api.nvim_win_set_config
local popups = {}
local last_pane = api.nvim_get_current_win()

local function pane(win)
  local seen = {}
  while win and api.nvim_win_is_valid(win) and not seen[win] do
    seen[win] = true
    local c = api.nvim_win_get_config(win)
    if c.relative == "" then
      return win
    end
    win = popups[win] and popups[win].owner or c.win
  end
  if api.nvim_win_is_valid(last_pane) then
    return last_pane
  end
end

local function borders(c)
  local b = c.border or vim.o.winborder or "none"
  if type(b) == "string" then
    if b == "" or b == "none" then return 0, 0 end
    if b == "shadow" then return 1, 1 end
    return 2, 2
  end
  local function present(i)
    local value = b[(i - 1) % #b + 1]
    if type(value) == "table" then value = value[1] end
    return value and value ~= ""
  end
  if #b == 0 then return 0, 0 end
  local left = present(1) or present(7) or present(8)
  local right = present(3) or present(4) or present(5)
  local top = present(1) or present(2) or present(3)
  local bottom = present(5) or present(6) or present(7)
  return (left and 1 or 0) + (right and 1 or 0), (top and 1 or 0) + (bottom and 1 or 0)
end

local function axis(pos, size, border, extent, source, far)
  local full = size + border
  local start = pos - (far and full or 0)
  local fitted = math.max(1, math.min(size, extent - border))
  local room = math.max(0, extent - fitted - border)
  if source then
    -- Preserve placement within the available room, so centered dialogs stay centered.
    start = source > full and start / (source - full) * room or 0
  end
  return math.floor(math.max(0, math.min(room, start))), fitted
end

local function project(c, owner)
  local out = vim.deepcopy(c)
  local parent = c.relative == "editor" and owner or c.win
  if not parent or not api.nvim_win_is_valid(parent) then return out end
  local w, h = api.nvim_win_get_width(parent), api.nvim_win_get_height(parent)
  local bw, bh = borders(c)
  if w <= bw or h <= bh then
    out.border, bw, bh = "none", 0, 0
  end
  local anchor = c.anchor or "NW"
  local editor = c.relative == "editor"
  out.col, out.width = axis(
    c.col or 0, c.width, bw, w, editor and vim.o.columns or nil, anchor:sub(2, 2) == "E"
  )
  out.row, out.height = axis(
    c.row or 0, c.height, bh, h, editor and vim.o.lines or nil, anchor:sub(1, 1) == "S"
  )
  out.relative, out.win, out.anchor = "win", parent, "NW"
  return out
end

local function request(config, previous)
  if previous then
    return vim.tbl_extend("force", previous, config)
  end
  return vim.deepcopy(config)
end

local function owner_for(config, previous)
  local parent = config.relative == "win" and popups[config.win]
  return parent and parent.owner or previous and previous.owner or pane(api.nvim_get_current_win())
end

api.nvim_open_win = function(buf, enter, config)
  local owner = owner_for(config)
  local managed = owner and (config.relative == "editor" or (config.relative == "win" and popups[config.win]))
  local win = open_win(buf, enter, managed and project(config, owner) or config)
  if managed then
    popups[win] = { source = vim.deepcopy(config), owner = owner }
  end
  return win
end

api.nvim_win_set_config = function(win, config)
  if win == 0 then win = api.nvim_get_current_win() end
  local previous = popups[win]
  local source = request(config, previous and previous.source)
  local owner = owner_for(source, previous)
  local managed = owner and (source.relative == "editor" or (source.relative == "win" and popups[source.win]))
  if managed and not previous then
    local current = api.nvim_win_get_config(win)
    for _, key in ipairs({ "width", "height", "row", "col", "anchor", "border" }) do
      if source[key] == nil then source[key] = current[key] end
    end
  end
  set_config(win, managed and project(source, owner) or config)
  popups[win] = managed and { source = source, owner = owner } or nil
end

api.nvim_create_autocmd("WinEnter", {
  group = "rex_nvim",
  callback = function()
    local win = api.nvim_get_current_win()
    if api.nvim_win_get_config(win).relative == "" then last_pane = win end
  end,
})
api.nvim_create_autocmd("WinClosed", {
  group = "rex_nvim",
  callback = function(event) popups[tonumber(event.match)] = nil end,
})
api.nvim_create_autocmd({ "WinResized", "VimResized" }, {
  group = "rex_nvim",
  callback = function()
    local windows = vim.tbl_keys(popups)
    table.sort(windows)
    for _, win in ipairs(windows) do
      local p = popups[win]
      if api.nvim_win_is_valid(win) and api.nvim_win_is_valid(p.owner) then
        set_config(win, project(p.source, p.owner))
      end
    end
  end,
})
