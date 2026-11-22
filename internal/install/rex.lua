-- Written by `rig install`, which rewrites it; `rig uninstall` removes it.
local rig = RIG_BINARY
local terminal = "com.superlogical.terminal"

-- The folder a pane works in. For a search, that of the shell, rather than
-- of whatever runs in it; for paths a program printed, the program's own.
local function folder(ctx, program_first)
  local p = rex.call(terminal .. ".process", { session_id = ctx.session_id, block_id = ctx.block_id })
  if not p then return end
  local function program()
    for _, kind in ipairs{ "foreground", "child" } do
      local q = p[kind]
      if q and q.cwd then return q.cwd end
    end
  end
  if program_first and program() then return program() end
  for i = #(p.ancestors or {}), 1, -1 do
    local a = p.ancestors[i]
    if a.argv0 and a.argv0:sub(1, 1) == "-" and a.cwd then return a.cwd end
  end
  return program()
end

-- Opens a rig popup over the focused pane's tab, with argv(cwd). An open
-- popup with the same label is brought forward, or with fresh replaced.
local function popup(ctx, label, argv, fresh, program_first)
  if not ctx.block_id then error("Focus a pane in a project first.", 0) end
  local view, err = rex.session.view{ session_id = ctx.session_id }
  if not view then error(err or "Could not read this session.", 0) end
  local window
  for _, w in ipairs(view.windows or {}) do
    for _, layer in ipairs(w.layers or {}) do
      for _, b in ipairs(layer.blocks or {}) do
        if b.block_id == ctx.block_id then window = w end
      end
    end
  end
  if not window then error("Could not find this tab.", 0) end

  local stale = {}
  for _, layer in ipairs(window.layers or {}) do
    for _, b in ipairs(layer.blocks or {}) do
      if b.label == label and b.block_id == ctx.block_id and fresh then
        -- Pressed in the popup itself: close it, rather than list itself.
        rex.call("block.close", { session_id = ctx.session_id, block_id = b.block_id, keep_last = true })
        return
      elseif b.label == label and fresh then
        stale[#stale + 1] = b.block_id
      elseif b.label == label then
        rex.call("session.set_layer_state", { session_id = ctx.session_id, layer_id = layer.layer_id, state = "normal" })
        rex.session.raise_layer{ session_id = ctx.session_id, layer_id = layer.layer_id }
        rex.session.focus_block{ session_id = ctx.session_id, block_id = b.block_id }
        return
      end
    end
  end

  local cwd = folder(ctx, program_first)
  if not cwd then error("Could not tell which folder this pane is in.", 0) end
  local made, nerr = rex.session.new_layer{
    session_id = ctx.session_id, window_id = window.window_id, focus = true,
    bounds = { 0.15, 0.1, 0.7, 0.75 },
    layout = { block = { flavor = terminal .. ".shell", label = label,
      options = { command = argv(cwd), cwd = cwd } } },
  }
  if not made then error(nerr or "Rex could not open " .. label .. ".", 0) end
  for _, id in ipairs(stale) do
    rex.call("block.close", { session_id = ctx.session_id, block_id = id, keep_last = true })
  end
end

local function find(ctx)
  -- One search per tab: bring an open one forward.
  popup(ctx, "rig find", function(cwd) return { rig, "find", cwd } end)
end

rex.action{
  name = "rig_find", title = "rig: Find Files",
  description = "Search file names and text in this pane's project, and open a result.",
  run = find,
}
rex.bind("shift+cmd+f", "rig_find")

-- The files the focused pane has mentioned, such as those Claude Code read,
-- changed or pointed to. Read afresh each time, so the list is current.
local function mentions(ctx)
  if ctx.block_id == nil then error("Focus a pane first.", 0) end
  local label = "rig mentions"
  local session, block = ctx.session_id, ctx.block_id
  popup(ctx, label, function(cwd)
    return { rig, "mentions", "--session", session, "--block", block, cwd }
  end, true, true)
end

rex.action{
  name = "rig_mentions", title = "rig: Files Mentioned Here",
  description = "List the files this pane has mentioned, newest first, and open one in rig view.",
  run = mentions,
}
rex.bind("shift+cmd+o", "rig_mentions")

-- The label of a block, if this tab has it.
local function label_of(ctx)
  local view = rex.session.view{ session_id = ctx.session_id }
  for _, w in ipairs(view and view.windows or {}) do
    for _, layer in ipairs(w.layers or {}) do
      for _, b in ipairs(layer.blocks or {}) do
        if b.block_id == ctx.block_id then return b.label end
      end
    end
  end
end

-- Shift+Cmd+D is Rex's Split Pane Down. In a rig viewer it switches between
-- the file and its change instead; everywhere else it still splits.
local view_label = "rig view: "
local function toggle_diff(ctx)
  local label = ctx.block_id and label_of(ctx)
  if label and label:sub(1, #view_label) == view_label then
    local _, err = rex.call(terminal .. ".write", { session_id = ctx.session_id, block_id = ctx.block_id,
      args = { data = "d" } })
    if err then error(err, 0) end
    return
  end
  rex.client.queue("pane.split.down")
end

rex.action{
  name = "rig_toggle_diff", title = "rig: Toggle Diff",
  description = "In a rig viewer, switch between the file and its change. Elsewhere, split the pane down.",
  run = toggle_diff,
}
rex.bind("shift+cmd+d", "rig_toggle_diff")
