-- Builds rig's sidebar beside the terminal rig runs in. Rex lays a new
-- split out evenly whatever ratio it is given, so the panes are created
-- first and sized afterwards. Any sidebar already in the tab is replaced.
local a = rex.args
local sid, main = rex.session_id, rex.block_id

local function window_of(block)
  local view, err = rex.session.view{ session_id = sid }
  if not view then error(err or "could not read the session", 0) end
  for _, w in ipairs(view.windows or {}) do
    for _, layer in ipairs(w.layers or {}) do
      for _, b in ipairs(layer.blocks or {}) do
        if b.block_id == block then return w end
      end
    end
  end
  error("could not find this tab", 0)
end

local ours = {}
for _, p in ipairs(a.panes) do ours["rig:" .. p.pane] = true end
ours["rig:outline"], ours["rig:history"], ours["rig:usage"] = true, true, true

for _, layer in ipairs(window_of(main).layers or {}) do
  for _, b in ipairs(layer.blocks or {}) do
    if ours[b.label] then
      rex.call("block.close", { session_id = sid, block_id = b.block_id, keep_last = true })
    end
  end
end

-- The sidebar spans the whole tab, beside everything already in it.
local root
for _, layer in ipairs(window_of(main).layers or {}) do
  if layer.kind == "tiled" and layer.layout then
    root = layer.layout.block_id or (layer.layout.split and layer.layout.split.split_id)
  end
end

local function pane(p)
  return rex.layout.block{ flavor = "com.superlogical.terminal.shell", label = "rig:" .. p.pane,
    options = { command = { a.rig, p.pane }, cwd = a.cwd } }
end
local stack = pane(a.panes[#a.panes])
for i = #a.panes - 1, 1, -1 do
  stack = rex.layout.vertical(0.5, pane(a.panes[i]), stack)
end

local left = a.side == "left"
local made, err = rex.session.new_split{
  session_id = sid, anchor_id = root or main, direction = "horizontal",
  side = left and "before" or "after", focus = false, layout = stack,
}
if not (made and made.block_ids and #made.block_ids == #a.panes) then
  error(err or "Rex could not add the sidebar", 0)
end
local ids = made.block_ids

local function size_of(block)
  local size = rex.call("com.superlogical.terminal.size", { session_id = sid, block_id = block })
  -- Before an app has measured a pane, Rex reports a fallback size.
  if size and type(size.owner) == "string" and size.rows and size.rows > 0 then return size end
end

local function rows_of(block)
  local size = size_of(block)
  return size and size.rows
end

-- An app that is not drawing the tab, such as one behind another window,
-- leaves new panes at the size they were made with. A sidebar pane as wide
-- as the main pane has not been measured since.
local function measured(block)
  local size, beside = size_of(block), size_of(main)
  if size and beside and (size.columns or 0) < (beside.columns or 0) then return size.rows end
end

-- Each pane's share of the sidebar's height. A pane sized in rows starts
-- from an estimate, corrected below once the panes can be measured.
local tall = rows_of(main) or 40
local share = {}
local function shares()
  local fixed = 0
  for i, p in ipairs(a.panes) do
    if p.rows and p.rows > 0 then
      share[i] = share[i] or (p.rows + 1) / tall
      fixed = fixed + share[i]
    end
  end
  for i, p in ipairs(a.panes) do
    if not (p.rows and p.rows > 0) then share[i] = math.max(1 - fixed, 0.05) * p.height end
  end
end

-- The sizes to set: each border, as the blocks either side of it and the
-- share of its split that goes before it.
local borders
local function plan()
  shares()
  borders = {}
  if left then
    borders[1] = { "horizontal", ids[1], main, a.width }
  else
    borders[1] = { "horizontal", main, ids[1], 1 - a.width }
  end
  -- Each pane's border with the next divides what is left of the sidebar.
  local rest = 1
  for i = 1, #ids - 1 do
    borders[#borders + 1] = { "vertical", ids[i], ids[i + 1], math.min(math.max(share[i] / rest, 0.01), 0.99) }
    rest = rest - share[i]
  end
end

local function contains(node, id)
  if not node then return false end
  if node.block_id then return node.block_id == id end
  return contains(node.split.before, id) or contains(node.split.after, id)
end

-- ratio finds the split whose border lies between two blocks.
local function ratio(node, direction, before, after)
  local split = node and node.split
  if not split then return end
  if split.direction == direction and contains(split.before, before) and contains(split.after, after) then
    return split.ratio
  end
  return ratio(split.before, direction, before, after) or ratio(split.after, direction, before, after)
end

local function settled()
  local tiled
  for _, layer in ipairs(window_of(main).layers or {}) do
    if layer.kind == "tiled" then tiled = layer.layout end
  end
  for _, b in ipairs(borders) do
    local r = ratio(tiled, b[1], b[2], b[3])
    if not r or math.abs(r - b[4]) > 0.0005 then return false end
  end
  return true
end

local function apply()
  for _, b in ipairs(borders) do
    local _, rerr = rex.session.resize_pane{ session_id = sid, direction = b[1],
      before_block_id = b[2], after_block_id = b[3], ratio = b[4] }
    if rerr then error(rerr, 0) end
  end
end

-- A Rex app showing the tab evens out new panes once it sees them,
-- overwriting sizes set too early. Set them, and set them again whenever
-- they are lost, until they have held for a while.
local function hold()
  local held, waited = 0, 0
  while held < 8 and waited < 4 do
    if settled() then
      held = held + 1
    else
      held = 0
      apply()
    end
    rex.sleep(0.1)
    waited = waited + 0.1
  end
end

plan()
hold()

-- A share of the height is not an exact number of rows: each pane's header
-- and padding take some of it. Measure each pane sized in rows and correct
-- its share until it has exactly the rows asked for. Only a fresh
-- measurement is trusted, and a correction stays near the estimate, so an
-- app that is not drawing the tab leaves the estimate in place.
local estimate, last = {}, {}
for i in pairs(share) do estimate[i] = share[i] end
for _ = 1, 8 do
  local off = false
  for i, p in ipairs(a.panes) do
    local got = p.rows and p.rows > 0 and measured(ids[i])
    if got and got ~= p.rows and got ~= last[i] then
      off = true
      last[i] = got
      share[i] = math.min(math.max(share[i] + (p.rows - got) / tall, estimate[i] / 2), estimate[i] * 2)
    end
  end
  if not off then break end
  plan()
  apply()
  rex.sleep(0.25)
end
rex.session.focus_block{ session_id = sid, block_id = main }
print("ok")
