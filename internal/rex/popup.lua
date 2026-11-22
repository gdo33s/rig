-- Shows a program over the tab that rig runs in, as a floating layer, or a
-- new panel when Rex cannot float one. A popup already showing the same
-- label is brought to the front instead of starting a second copy, because
-- two editors on one file fight over its swap file; with fresh, it is
-- replaced instead, as a read-only view can be. Prints what it did:
-- "float", "split" or "focus", then the block.
local a = rex.args
local sid = rex.session_id

local view, err = rex.session.view{ session_id = sid }
if not view then error(err or "could not read the session", 0) end

local window
for _, w in ipairs(view.windows or {}) do
  for _, layer in ipairs(w.layers or {}) do
    for _, b in ipairs(layer.blocks or {}) do
      if b.block_id == rex.block_id then window = w end
    end
  end
end
if not window then error("could not find this tab", 0) end

local tiled, replaceable = nil, {}
for _, layer in ipairs(window.layers or {}) do
  if layer.kind == "tiled" then tiled = layer end
  for _, b in ipairs(layer.blocks or {}) do
    if b.label == a.label and not a.fresh then
      if layer.kind == "floating" then
        rex.call("session.set_layer_state", { session_id = sid, layer_id = layer.layer_id, state = "normal" })
        rex.session.raise_layer{ session_id = sid, layer_id = layer.layer_id }
      end
      local _, ferr = rex.session.focus_block{ session_id = sid, block_id = b.block_id }
      if ferr then error(ferr, 0) end
      print("focus " .. b.block_id)
      return
    end
    if a.replaces and b.label and b.label:sub(1, #a.replaces) == a.replaces then
      replaceable[#replaceable + 1] = b.block_id
    end
  end
end

local layout = { block = {
  flavor = "com.superlogical.terminal.shell",
  label = a.label,
  options = { command = a.command, cwd = a.cwd },
} }

local how = "float"
local made = rex.session.new_layer{
  session_id = sid, window_id = window.window_id, focus = true,
  bounds = { 0.10, 0.07, 0.80, 0.86 }, layout = layout,
}
if not (made and made.block_ids and made.block_ids[1]) then
  -- Beside the whole tab, so a narrow sidebar is not split further.
  local root = tiled and tiled.layout
  local anchor = root and (root.block_id or (root.split and root.split.split_id)) or rex.block_id
  how = "split"
  made, err = rex.session.new_split{
    session_id = sid, anchor_id = anchor, direction = "horizontal",
    side = "after", ratio = 0.45, focus = true, layout = layout,
  }
end
if not (made and made.block_ids and made.block_ids[1]) then
  error(err or "Rex could not open the file", 0)
end

-- Only read-only popups are replaced, and only once the new one exists.
for _, id in ipairs(replaceable) do
  rex.call("block.close", { session_id = sid, block_id = id, keep_last = true })
end
print(how .. " " .. made.block_ids[1])
