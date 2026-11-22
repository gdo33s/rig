package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gdo33s/rig/internal/find"
	"github.com/gdo33s/rig/internal/git"
	"github.com/gdo33s/rig/internal/history"
	"github.com/gdo33s/rig/internal/install"
	"github.com/gdo33s/rig/internal/layout"
	"github.com/gdo33s/rig/internal/mentions"
	"github.com/gdo33s/rig/internal/outline"
	"github.com/gdo33s/rig/internal/rex"
	"github.com/gdo33s/rig/internal/usage"
	"github.com/gdo33s/rig/internal/view"
)

const help = `rig: Outline, History and Usage panes for Rex

Usage:
  rig                  arrange this Rex tab as ~/.rig/layout.json says:
                       a sidebar of rig panes, and this terminal running
                       the main command beside it
  rig outline [PATH]   Git-aware file tree
  rig history [PATH]   files changed since the last commit; Tab for history
  rig usage            Claude Code limits and context, and the branch's diff
  rig find [PATH]      search file names and text with fff, and open a result
  rig mentions --session ID --block ID [PATH]
                       the files a Rex terminal has mentioned, newest
                       first, to open in rig view
  rig view [--line N | --diff | --commit SHA] FILE
                       show a file, or its change, with syntax colours
  rig install          add rig to Rex: shift+cmd+f opens rig find in any
                       pane, and shift+cmd+o the files it has mentioned
  rig uninstall        remove rig from Rex
  rig statusline [--then CMD]
                       Claude Code status-line command: records rate limits
                       for rig usage, then prints CMD's output or rig's line
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "rig:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return arrange()
	}
	switch args[0] {
	case "outline":
		dir, err := dirArg(args[1:])
		if err != nil {
			return err
		}
		return runPane(paneOf(outline.New(dir)))
	case "history":
		dir, err := dirArg(args[1:])
		if err != nil {
			return err
		}
		return runPane(paneOf(history.New(dir)))
	case "usage":
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		m := usage.New(dir)
		if !m.UsesImages() {
			return runPane(paneOf(m))
		}
		// Images belong to the screen they were sent on, so enter the
		// alternate screen first and send them there.
		os.Stdout.WriteString("\x1b[?1049h")
		_ = m.SendIcons(os.Stdout)
		err = runLive(paneOf(m))
		os.Stdout.WriteString("\x1b[?1049l")
		return err
	case "find":
		dir, err := dirArg(args[1:])
		if err != nil {
			return err
		}
		return find.Run(dir)
	case "view":
		return runView(args[1:])
	case "mentions":
		return runMentions(args[1:])
	case "install":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		p := install.DefaultPaths()
		if err := install.Install(p, exe); err != nil {
			return err
		}
		fmt.Printf("rig: added to Rex. shift+cmd+f opens rig find; shift+cmd+o lists the files a pane mentioned; shift+cmd+d switches a rig view between file and diff.\n  %s\n  loaded from %s\n", p.Lua, p.Init)
		return nil
	case "uninstall":
		p := install.DefaultPaths()
		if err := install.Uninstall(p); err != nil {
			return err
		}
		fmt.Printf("rig: removed from Rex (%s).\n", p.Init)
		return nil
	case "statusline":
		var then string
		if len(args) == 3 && args[1] == "--then" {
			then = args[2]
		} else if len(args) != 1 {
			return fmt.Errorf("usage: rig statusline [--then CMD]")
		}
		return usage.StatusLine(os.Stdin, os.Stdout, then)
	case "-h", "--help", "help":
		fmt.Print(help)
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}

// arrange builds the sidebar the layout file describes beside this
// terminal, then replaces rig with the main command, so this terminal
// becomes the main pane.
func arrange() error {
	target, ok := rex.Current()
	if !ok {
		return fmt.Errorf("run rig in a Rex terminal to arrange its tab, or see rig help")
	}
	path := layout.Path()
	cfg, wrote, err := layout.Load(path)
	if err != nil {
		return err
	}
	if wrote {
		fmt.Fprintf(os.Stderr, "rig: wrote a starting layout to %s\n", path)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	sidebar := rex.Sidebar{Side: cfg.Sidebar.Side, Width: cfg.Sidebar.Width / 100, Rig: exe, Cwd: cwd}
	for i, h := range cfg.Sidebar.Heights() {
		p := cfg.Sidebar.Panes[i]
		sidebar.Panes = append(sidebar.Panes, rex.SidebarPane{Pane: p.Pane, Height: h, Rows: p.Rows})
	}
	if err := rex.OpenSidebar(context.Background(), target, sidebar); err != nil {
		return err
	}
	// Name the session after the project rather than leave Rex's random
	// words. A failure here leaves the name as it was and nothing else.
	if err := rex.SetSessionLabel(context.Background(), target, projectName(cwd)); err != nil {
		fmt.Fprintln(os.Stderr, "rig: could not name the session:", err)
	}
	if cfg.Main.Command == "" {
		return nil
	}
	// Replace rig with the command, through sh so it can carry arguments and
	// is found on the shell's PATH. Quitting it returns to the shell rig was
	// run from.
	return syscall.Exec("/bin/sh", []string{"sh", "-c", cfg.Main.Command}, os.Environ())
}

// runView reads view's flags and shows the file. The file is taken
// relative to the repository it is in, so the viewer can ask git about it.
func runView(args []string) error {
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	line := fs.Int("line", 0, "line to start at")
	diff := fs.Bool("diff", false, "show the change since the last commit")
	commit := fs.String("commit", "", "show the change in this commit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: rig view [--line N | --diff | --commit SHA] FILE")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	file := fs.Arg(0)
	if !filepath.IsAbs(file) {
		file = filepath.Join(cwd, file)
	}
	root := filepath.Dir(file)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if repo, err := git.Open(ctx, root); err == nil {
		root = repo.Root
	}
	cancel()
	// git reports the root with symlinks resolved; match the file to it.
	if real, err := filepath.EvalSymlinks(file); err == nil {
		file = real
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	rel, err := filepath.Rel(root, file)
	if err != nil {
		return err
	}
	return view.Run(view.Options{Root: root, Path: rel, Line: *line, Diff: *diff, Commit: *commit})
}

// runMentions lists the files a terminal has mentioned. Paths in it start
// from PATH, the folder its program runs in.
func runMentions(args []string) error {
	fs := flag.NewFlagSet("mentions", flag.ContinueOnError)
	session := fs.String("session", "", "the Rex session the terminal is in")
	block := fs.String("block", "", "the terminal's block")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *session == "" || *block == "" || fs.NArg() > 1 {
		return fmt.Errorf("usage: rig mentions --session ID --block ID [PATH]")
	}
	dir, err := dirArg(fs.Args())
	if err != nil {
		return err
	}
	return mentions.Run(mentions.Source{Session: *session, Block: *block, Dir: dir})
}

// projectName is the name of the repository dir is in, or of dir itself
// outside one, so a tab opened in a subfolder is named after the project.
func projectName(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if repo, err := git.Open(ctx, dir); err == nil {
		dir = repo.Root
	}
	return filepath.Base(dir)
}

func dirArg(args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	return os.Getwd()
}

// runPane runs a sidebar pane until it is quit. Panes run for hours, so
// one that finds rig rebuilt under it restarts itself with the new build,
// in place, rather than carry on with old behaviour.
func runPane(m tea.Model) error {
	return runLive(m, tea.WithAltScreen())
}

func runLive(m tea.Model, opts ...tea.ProgramOption) error {
	p := tea.NewProgram(m, append([]tea.ProgramOption{tea.WithMouseCellMotion()}, opts...)...)
	exe, rebuilt := watchRebuild(p)
	_, err := p.Run()
	if err == nil && rebuilt.Load() {
		return syscall.Exec(exe, os.Args, os.Environ())
	}
	return err
}

// watchRebuild quits p once the rig binary has been replaced and has stayed
// unchanged for a moment, so a build still being written is not run.
func watchRebuild(p *tea.Program) (string, *atomic.Bool) {
	rebuilt := new(atomic.Bool)
	exe, err := os.Executable()
	if err != nil {
		return exe, rebuilt
	}
	stamp := func() string {
		info, err := os.Stat(exe)
		if err != nil || info.Size() == 0 {
			return ""
		}
		return fmt.Sprint(info.ModTime().UnixNano(), info.Size())
	}
	started := stamp()
	go func() {
		last := started
		for range time.Tick(2 * time.Second) {
			now := stamp()
			if now != "" && now != started && now == last {
				rebuilt.Store(true)
				p.Quit()
				return
			}
			last = now
		}
	}()
	return exe, rebuilt
}

// pane adapts a model whose Update returns its own type to tea.Model.
type pane[M interface {
	Init() tea.Cmd
	Update(tea.Msg) (M, tea.Cmd)
	View() string
}] struct{ m M }

func paneOf[M interface {
	Init() tea.Cmd
	Update(tea.Msg) (M, tea.Cmd)
	View() string
}](m M) pane[M] {
	return pane[M]{m}
}

func (p pane[M]) Init() tea.Cmd { return p.m.Init() }
func (p pane[M]) View() string  { return p.m.View() }
func (p pane[M]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	p.m, cmd = p.m.Update(msg)
	return p, cmd
}
