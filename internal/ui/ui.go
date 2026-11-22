// Package ui holds the styles and text helpers the panes share. Colours are
// the terminal's own 16 ANSI colours, so rig follows whatever Rex theme is
// active instead of imposing one.
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/gdo33s/rig/internal/git"
)

var (
	Dim      = lipgloss.NewStyle().Faint(true)
	Bold     = lipgloss.NewStyle().Bold(true)
	Selected = lipgloss.NewStyle().Reverse(true)
	Error    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	Added    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	Deleted  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	Heading  = lipgloss.NewStyle().Bold(true)
	Hash     = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	Accent   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
)

// KindStyle colours a change by its single-letter kind.
func KindStyle(kind byte) lipgloss.Style {
	s := lipgloss.NewStyle()
	switch kind {
	case 'A', '?':
		return s.Foreground(lipgloss.Color("2"))
	case 'M', 'T':
		return s.Foreground(lipgloss.Color("3"))
	case 'D':
		return s.Foreground(lipgloss.Color("1"))
	case 'R', 'C':
		return s.Foreground(lipgloss.Color("4"))
	case 'U':
		return s.Foreground(lipgloss.Color("5"))
	}
	return s
}

// Line lays out left and right text on one line of exactly width cells,
// truncating the left side first. The right side is dropped when there is
// no room for it.
func Line(left, right string, width int) string {
	if width <= 0 {
		return ""
	}
	rw := ansi.StringWidth(right)
	if right == "" || rw+2 > width/2 {
		right, rw = "", 0
	}
	room := width - rw
	if rw > 0 {
		room--
	}
	left = ansi.Truncate(left, room, "…")
	gap := width - ansi.StringWidth(left) - rw
	return left + strings.Repeat(" ", max(gap, 0)) + right
}

// Plain strips styling, so a selected row can be drawn in one style.
func Plain(s string) string { return ansi.Strip(s) }

// Count renders a line delta such as "+12 -3". Unknown counts render as "?".
func Count(added, deleted int, pending bool) string {
	if pending {
		return Dim.Render("?")
	}
	var parts []string
	if added > 0 {
		parts = append(parts, Added.Render(fmt.Sprintf("+%d", added)))
	}
	if deleted > 0 {
		parts = append(parts, Deleted.Render(fmt.Sprintf("-%d", deleted)))
	}
	return strings.Join(parts, " ")
}

// Age is a compact time since t, such as "5m", "3h", "2d" or "1y".
func Age(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	}
	return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
}

// Spread puts left and right at the two ends of a line of width cells,
// truncating left to make room. Right is dropped only if it cannot fit at
// all.
func Spread(left, right string, width int) string {
	rw := ansi.StringWidth(right)
	if rw+1 > width {
		right, rw = "", 0
	}
	room := width - rw
	if rw > 0 {
		room--
	}
	left = ansi.Truncate(left, room, "…")
	return left + strings.Repeat(" ", max(width-ansi.StringWidth(left)-rw, 0)) + right
}

// ChangeStats is a changed file's line delta and its kind letter, such as
// "+12 -3 M", as the panes show at the end of a file's line.
func ChangeStats(c *git.Change) string {
	kind := KindStyle(c.Kind()).Render(string(c.Kind()))
	switch {
	case c.Binary:
		return Dim.Render("bin ") + kind
	case c.Added < 0:
		return Dim.Render("? ") + kind
	}
	if s := Count(c.Added, c.Deleted, false); s != "" {
		return s + " " + kind
	}
	return kind
}
