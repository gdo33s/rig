package ui

import tea "github.com/charmbracelet/bubbletea"

// Scroll is a cursor in a list of n rows and the window of rows on screen.
type Scroll struct {
	Cursor int
	Offset int
}

// Clamp keeps the cursor on a row and the window around the cursor.
func (s *Scroll) Clamp(n, height int) {
	height = max(height, 1)
	s.Cursor = min(max(s.Cursor, 0), max(n-1, 0))
	if s.Cursor < s.Offset {
		s.Offset = s.Cursor
	}
	if s.Cursor >= s.Offset+height {
		s.Offset = s.Cursor - height + 1
	}
	s.Offset = max(min(s.Offset, n-height), 0)
}

// Move handles the movement keys every list shares, reporting whether key
// was one of them.
func (s *Scroll) Move(key string, n, height int) bool {
	switch key {
	case "up", "k", "ctrl+p":
		s.Cursor--
	case "down", "j", "ctrl+n":
		s.Cursor++
	case "pgup", "ctrl+u":
		s.Cursor -= max(height/2, 1)
	case "pgdown", "ctrl+d":
		s.Cursor += max(height/2, 1)
	case "home", "g":
		s.Cursor = 0
	case "end", "G":
		s.Cursor = n - 1
	default:
		return false
	}
	s.Clamp(n, height)
	return true
}

// Wheel scrolls for a mouse wheel event, reporting whether it was one.
func (s *Scroll) Wheel(msg tea.MouseMsg, n, height int) bool {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		s.Cursor -= 3
	case tea.MouseButtonWheelDown:
		s.Cursor += 3
	default:
		return false
	}
	s.Clamp(n, height)
	return true
}

// Visible is the range of rows on screen.
func (s Scroll) Visible(n, height int) (start, end int) {
	return s.Offset, min(s.Offset+max(height, 1), n)
}

// Keys splits keys typed faster than one read, which arrive as a single
// message, so each can be handled on its own. Pastes stay whole.
func Keys(msg tea.KeyMsg) []tea.KeyMsg {
	if msg.Type != tea.KeyRunes || len(msg.Runes) < 2 || msg.Paste {
		return []tea.KeyMsg{msg}
	}
	keys := make([]tea.KeyMsg, len(msg.Runes))
	for i, r := range msg.Runes {
		keys[i] = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: msg.Alt}
	}
	return keys
}
