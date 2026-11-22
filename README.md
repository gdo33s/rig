# rig

Side panes for the [Rex](https://www.superlogical.com/rex) terminal, for working with Claude Code:

- **Outline**: a Git-aware file tree.
- **History**: the files changed since the last commit, and the commit history a Tab away.
- **Usage**: Claude Code's 5-hour and weekly limits, the session's context, and the branch's diff.

It also adds:

- **Find** (⇧⌘F): file names and text, searched with fff.
- **Mentions** (⇧⌘O): the files a pane has mentioned, such as those Claude read or changed.
- **A viewer** with syntax colours and side-by-side diffs, which opens files from all of the above in a popup.

Running `rig` in a Rex tab lays it out: a sidebar of the three panes on the left, and the terminal you ran it in becomes the main pane, running `claude`.

## Install

You need Go 1.21 or later (it downloads the newer toolchain rig needs) and git. Rex is needed for the popups and the sidebar; the panes themselves run in any terminal.

```sh
go install github.com/gdo33s/rig/cmd/rig@latest
```

This puts `rig` in `$(go env GOPATH)/bin`, usually `~/go/bin`. Make sure that folder is on your `PATH`.

Then add rig's shortcuts to Rex:

```sh
rig install
```

This writes `~/.config/rig/rex.lua`, adds one line to Rex's `init.lua` to load it, reloads Rex's config, and installs `fff-mcp` for file search if it's missing. `rig uninstall` removes both. Run `rig install` again after upgrading rig, so Rex picks up any new shortcuts.

### Claude Code usage

The Usage pane reads Claude Code's limits through its status line. Add this to `~/.claude/settings.json`:

```json
{
  "statusLine": { "type": "command", "command": "rig statusline" }
}
```

If you already have a status line command, keep it and let rig pass its output through:

```json
{
  "statusLine": { "type": "command", "command": "rig statusline --then 'your-command'" }
}
```

Use the full path, such as `~/go/bin/rig`, if Claude Code doesn't find `rig` on its `PATH`.

### File search

Find uses [fff](https://github.com/dmtrKovalenko/fff)'s `fff-mcp` server. `rig install` downloads it to `~/.local/bin` when it isn't already installed, and checks the download against the release's checksums before installing it. If you already have `fff-mcp` on your `PATH`, rig uses that one.

## Use

In a Rex tab, in your project:

```sh
rig
```

The first run writes the layout to `~/.rig/layout.json`. Edit it to change the sidebar's side, width and panes, or the main command:

```json
{
  "sidebar": {
    "side": "left",
    "width": 15,
    "panes": [
      { "pane": "outline", "height": 50 },
      { "pane": "history", "height": 25 },
      { "pane": "usage", "rows": 8 }
    ]
  },
  "main": { "command": "claude" }
}
```

`width` and `height` are percentages; `rows` is an exact height. Running `rig` again replaces the sidebar. It also names the Rex session after the project, the folder of the Git repository you're in, instead of Rex's random words.

Each pane also runs on its own: `rig outline`, `rig history`, `rig usage`, `rig find`, and `rig view FILE`.

### Keys

| Where | Key | Does |
| --- | --- | --- |
| Anywhere in Rex | ⇧⌘F | Search file names and text in the pane's project |
| Anywhere in Rex | ⇧⌘O | List the files the pane has mentioned, newest first |
| Viewer | ⇧⌘D or `d` | Switch between the file and its diff |
| Outline | Enter, `h`/`l`, Space | Open a file, close or open a folder |
| Outline | `/`, `d`, `f` | Filter, open a file's diff, open Find |
| History | Tab | Switch between Changes and History |
| History | Enter | Open a change, or a commit's files |
| History | `i` | Show a commit's message |
| Viewer | `]`/`[` | Next or previous change |
| Viewer | `/`, `n`/`N` | Search |
| Viewer | `e` | Edit the file in `$EDITOR` |
| Lists | `q` or Esc | Close |

Panes restart themselves when the `rig` binary is replaced, so an upgrade shows up without rearranging the tab.
