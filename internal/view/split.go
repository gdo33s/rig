package view

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/gdo33s/rig/internal/git"
	"github.com/gdo33s/rig/internal/syntax"
	"github.com/gdo33s/rig/internal/ui"
)

// pair is one row of a side-by-side diff: a hunk heading, or a line on
// each side, either of which may be missing.
type pair struct {
	heading     *git.DiffLine
	left, right *git.DiffLine
	// Which runes changed, when a removed and an added line are close
	// enough for word marks to help; nil otherwise.
	leftWords, rightWords []bool
}

// pairs lays a diff out side by side, as lumen does: a run of removed
// lines is zipped with the run of added lines after it, row by row, and
// what is left over has nothing opposite.
func pairs(d git.FileDiff) []pair {
	var out []pair
	lines := d.Lines
	for i := 0; i < len(lines); {
		l := &lines[i]
		switch l.Kind {
		case '@':
			out = append(out, pair{heading: l})
			i++
		case ' ':
			out = append(out, pair{left: l, right: l})
			i++
		default:
			var gone, added []*git.DiffLine
			for ; i < len(lines) && lines[i].Kind == '-'; i++ {
				gone = append(gone, &lines[i])
			}
			for ; i < len(lines) && lines[i].Kind == '+'; i++ {
				added = append(added, &lines[i])
			}
			for j := range max(len(gone), len(added)) {
				var p pair
				if j < len(gone) {
					p.left = gone[j]
				}
				if j < len(added) {
					p.right = added[j]
				}
				if p.left != nil && p.right != nil {
					// Marks index runes as drawn, with tabs expanded.
					p.leftWords, p.rightWords = wordChanges(syntax.ExpandTabs(p.left.Text), syntax.ExpandTabs(p.right.Text))
				}
				out = append(out, p)
			}
		}
	}
	return out
}

// minUnchanged is how much of a changed pair must stay the same, ignoring
// spaces, for word marks to be worth showing; past that it is a different
// line, and marking every word would only be noise.
const minUnchanged = 0.20

// wordChanges marks, rune by rune, the words that differ between an old
// and a new line, or returns nil when too little is shared.
func wordChanges(old, new string) ([]bool, []bool) {
	a, b := words(old), words(new)
	same := lcs(a, b)
	shared, longest := 0, max(solid(old), solid(new))
	for i, keep := range same.left {
		if keep {
			shared += solid(a[i])
		}
	}
	if longest == 0 || float64(shared)/float64(longest) < minUnchanged {
		return nil, nil
	}
	return mask(a, same.left), mask(b, same.right)
}

// words splits a line into words (letters, digits, underscores), runs of
// spaces, and single other characters.
func words(s string) []string {
	var out []string
	r := []rune(s)
	for i := 0; i < len(r); {
		j := i + 1
		switch {
		case isWord(r[i]):
			for j < len(r) && isWord(r[j]) {
				j++
			}
		case unicode.IsSpace(r[i]):
			for j < len(r) && unicode.IsSpace(r[j]) {
				j++
			}
		}
		out = append(out, string(r[i:j]))
		i = j
	}
	return out
}

func isWord(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// solid counts the characters of s that are not spaces.
func solid(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

type kept struct{ left, right []bool }

// lcs finds the longest common subsequence of two word lists, marking the
// words of each that it keeps.
func lcs(a, b []string) kept {
	n, m := len(a), len(b)
	table := make([][]int, n+1)
	for i := range table {
		table[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else {
				table[i][j] = max(table[i+1][j], table[i][j+1])
			}
		}
	}
	k := kept{make([]bool, n), make([]bool, m)}
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case a[i] == b[j]:
			k.left[i], k.right[j] = true, true
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			i++
		default:
			j++
		}
	}
	return k
}

// mask turns kept words into a per-rune mark of the changed ones. Spaces
// are never marked, so a mark covers words, not the gaps between them.
func mask(ws []string, keep []bool) []bool {
	var out []bool
	for i, w := range ws {
		for _, r := range w {
			out = append(out, !keep[i] && !unicode.IsSpace(r))
		}
	}
	return out
}

// Backgrounds for changed lines and the words changed within them: lumen's
// defaults, chosen for a dark or light terminal background.
var (
	addedBg     = lipgloss.AdaptiveColor{Dark: "#233228", Light: "#E6FFED"}
	removedBg   = lipgloss.AdaptiveColor{Dark: "#322323", Light: "#FFF5F3"}
	addedWord   = lipgloss.AdaptiveColor{Dark: "#285537", Light: "#ABF2BC"}
	removedWord = lipgloss.AdaptiveColor{Dark: "#643232", Light: "#FFB8AE"}
)

// splitRow draws a side-by-side row: the old line on the left, the new on
// the right, a hunk heading across both.
func (m Model) splitRow(l line, selected bool) string {
	if l.mark == '@' {
		return ansi.Truncate("   "+l.text, m.width, "…")
	}
	half := (m.width - 1) / 2
	return m.splitCell(l.left, half, selected) + ui.Dim.Render("│") + m.splitCell(l.right, m.width-1-half, selected)
}

// splitCell draws one side: its line number, then its code, coloured, with
// the changed line and words shaded. A side with nothing is a faint stripe.
func (m Model) splitCell(c *cell, width int, selected bool) string {
	if c == nil {
		return ui.Dim.Render(strings.Repeat("╱", max(width, 0)))
	}
	digits := m.gutterWidth() - 2
	num := fmt.Sprintf(" %*d ", digits, c.num)
	if selected {
		num = ui.Selected.Render(num)
	} else {
		num = ui.Dim.Render(num)
	}
	var bg, word lipgloss.TerminalColor
	switch c.kind {
	case '-':
		bg, word = removedBg, removedWord
	case '+':
		bg, word = addedBg, addedWord
	}
	return num + code(c.spans, c.words, bg, word, m.hx, max(width-digits-2, 0))
}

// code draws spans from column hx for width columns, with bg behind them
// (when set) and word behind the runes words marks.
func code(spans []syntax.Span, words []bool, bg, word lipgloss.TerminalColor, hx, width int) string {
	var b strings.Builder
	col, drawn := 0, 0
	for _, sp := range spans {
		var run strings.Builder
		runMarked := false
		flush := func() {
			if run.Len() == 0 {
				return
			}
			st := sp.Style
			if bg != nil {
				if runMarked && word != nil {
					st = st.Background(word)
				} else {
					st = st.Background(bg)
				}
			}
			b.WriteString(st.Render(run.String()))
			run.Reset()
		}
		for _, r := range sp.Text {
			marked := col < len(words) && words[col]
			col++
			if col <= hx || drawn >= width {
				continue
			}
			if marked != runMarked {
				flush()
				runMarked = marked
			}
			run.WriteRune(r)
			drawn++
		}
		flush()
	}
	if pad := width - drawn; pad > 0 {
		fill := lipgloss.NewStyle()
		if bg != nil {
			fill = fill.Background(bg)
		}
		b.WriteString(fill.Render(strings.Repeat(" ", pad)))
	}
	return b.String()
}
