// Package syntax colours source code with chroma, using the terminal's own
// 16 colours so it follows whatever theme Rex has.
package syntax

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/lipgloss"
)

const tabWidth = 4

var (
	comment  = lipgloss.NewStyle().Faint(true)
	keyword  = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	str      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	number   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	function = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	typ      = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	attr     = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	plain    = lipgloss.NewStyle()
)

// style picks the colour for a kind of token. Most code is plain, so the
// colour that is there means something.
func style(t chroma.TokenType) lipgloss.Style {
	switch {
	case t.InCategory(chroma.Comment):
		return comment
	case t == chroma.KeywordType || t == chroma.NameClass || t == chroma.NameBuiltinPseudo:
		return typ
	case t.InCategory(chroma.Keyword), t == chroma.NameBuiltin, t == chroma.OperatorWord:
		return keyword
	case t.InSubCategory(chroma.LiteralString):
		return str
	case t.InSubCategory(chroma.LiteralNumber):
		return number
	case t == chroma.NameFunction || t == chroma.NameFunctionMagic:
		return function
	case t == chroma.NameTag || t == chroma.NameDecorator:
		return function
	case t == chroma.NameAttribute || t == chroma.NameConstant:
		return attr
	case t == chroma.GenericInserted:
		return str
	case t == chroma.GenericDeleted:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	case t == chroma.GenericHeading || t == chroma.GenericSubheading:
		return lipgloss.NewStyle().Bold(true)
	}
	return plain
}

// Lexer finds the lexer for a file by its name, then by its content,
// falling back to plain text.
func Lexer(name, src string) chroma.Lexer {
	l := lexers.Match(name)
	if l == nil {
		l = lexers.Analyse(src)
	}
	if l == nil {
		l = lexers.Fallback
	}
	return chroma.Coalesce(l)
}

// Language names a file's language for display, or "" for plain text.
func Language(name, src string) string {
	l := Lexer(name, src)
	if l.Config() == nil {
		return ""
	}
	switch n := l.Config().Name; n {
	case "plaintext", "fallback":
		return ""
	default:
		return n
	}
}

// ExpandTabs replaces tabs with spaces to the next tab stop.
func ExpandTabs(line string) string {
	if !strings.Contains(line, "\t") {
		return line
	}
	var b strings.Builder
	col := 0
	for _, r := range line {
		if r == '\t' {
			n := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String()
}

// Span is a run of text in one style.
type Span struct {
	Text  string
	Style lipgloss.Style
}

// Spans splits src into lines of styled runs, tabs expanded. Highlighting
// the whole file at once lets a lexer follow strings and comments that
// span lines.
func Spans(l chroma.Lexer, src string) [][]Span {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	raw := strings.Split(strings.TrimSuffix(src, "\n"), "\n")
	for i := range raw {
		raw[i] = ExpandTabs(raw[i])
	}
	out := make([][]Span, 0, len(raw))
	it, err := l.Tokenise(nil, strings.Join(raw, "\n")+"\n")
	if err != nil {
		for _, r := range raw {
			out = append(out, []Span{{Text: r, Style: plain}})
		}
		return out
	}
	var line []Span
	for tok := it(); tok != chroma.EOF; tok = it() {
		st := style(tok.Type)
		for i, part := range strings.Split(tok.Value, "\n") {
			if i > 0 {
				out = append(out, line)
				line = nil
			}
			if part != "" {
				line = append(line, Span{Text: part, Style: st})
			}
		}
	}
	if len(line) > 0 {
		out = append(out, line)
	}
	for len(out) < len(raw) {
		out = append(out, nil)
	}
	return out[:len(raw)]
}

// Render draws a line of spans.
func Render(spans []Span) string {
	var b strings.Builder
	for _, sp := range spans {
		b.WriteString(sp.Style.Render(sp.Text))
	}
	return b.String()
}

// Lines colours src and returns it as lines, tabs expanded.
func Lines(l chroma.Lexer, src string) []string {
	spans := Spans(l, src)
	out := make([]string, len(spans))
	for i, line := range spans {
		out[i] = Render(line)
	}
	return out
}

// Line colours a single line on its own, as for a diff, where lines come
// without the rest of the file.
func Line(l chroma.Lexer, line string) string {
	return Lines(l, line)[0]
}
