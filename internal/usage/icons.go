package usage

import (
	"errors"
	"image"
	"image/color"
	"io"
	"os"

	"github.com/muesli/termenv"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/gdo33s/rig/internal/kitty"
)

// How icons are drawn.
type iconMode int

const (
	textIcons  iconMode = iota // short labels: 5h, 7d, ctx
	fontIcons                  // Nerd Font glyphs, one cell each
	imageIcons                 // the same glyphs as images two rows tall
)

// Image icons span this many cells. Terminal cells are about twice as tall
// as wide, so this is roughly square.
const (
	iconCols = 4
	iconRows = 2
	iconPx   = 128
)

// fontPaths are where Rex keeps the Nerd Font it draws icons with.
var fontPaths = []string{
	"/Applications/Rex Beta.app/Contents/Resources/swift-ghostty_GhosttyRender.bundle/Contents/Resources/SymbolsNerdFont-Regular.ttf",
	"/Applications/Rex.app/Contents/Resources/swift-ghostty_GhosttyRender.bundle/Contents/Resources/SymbolsNerdFont-Regular.ttf",
}

// detectIcons picks how to draw icons. Rex shows Nerd Font glyphs and kitty
// images; elsewhere rig cannot know, so it uses labels. RIG_ICONS can be
// set to text, font or image to choose.
func detectIcons() iconMode {
	switch os.Getenv("RIG_ICONS") {
	case "text", "0":
		return textIcons
	case "font", "1":
		return fontIcons
	case "image":
		return imageIcons
	}
	if os.Getenv("REX_SESSION") == "" {
		return textIcons
	}
	return imageIcons
}

// UsesImages reports whether the pane wants SendIcons before it starts.
func (m Model) UsesImages() bool { return m.icons == imageIcons }

// SendIcons draws each icon from Rex's Nerd Font in the terminal's text
// colour and sends it to the terminal. It must run in the screen the pane
// will draw on, before the pane starts reading input. If it fails, the
// pane falls back to one-cell font icons.
func (m *Model) SendIcons(w io.Writer) error {
	face, err := iconFace()
	if err != nil {
		m.icons = fontIcons
		return err
	}
	fg := foreground()
	for _, it := range allItems {
		if err := kitty.Send(w, it.id, rasterize(face, it.icon, fg), iconCols, iconRows); err != nil {
			m.icons = fontIcons
			return err
		}
	}
	return nil
}

func iconFace() (font.Face, error) {
	for _, path := range fontPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		f, err := opentype.Parse(data)
		if err != nil {
			return nil, err
		}
		return opentype.NewFace(f, &opentype.FaceOptions{Size: iconPx * 0.8, DPI: 72, Hinting: font.HintingNone})
	}
	return nil, errors.New("Rex's Nerd Font not found")
}

// foreground asks the terminal for its text colour, so icons match it.
func foreground() color.Color {
	c := termenv.NewOutput(os.Stdout).ForegroundColor()
	if _, none := c.(termenv.NoColor); c == nil || none {
		return color.NRGBA{0xd0, 0xd0, 0xd0, 255}
	}
	r, g, b := termenv.ConvertToRGB(c).RGB255()
	return color.NRGBA{r, g, b, 255}
}

// rasterize draws glyph centred on a square transparent canvas.
func rasterize(face font.Face, glyph string, fg color.Color) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, iconPx, iconPx))
	d := &font.Drawer{Dst: img, Src: image.NewUniform(fg), Face: face}
	bounds, _ := d.BoundString(glyph)
	w := (bounds.Max.X - bounds.Min.X).Ceil()
	h := (bounds.Max.Y - bounds.Min.Y).Ceil()
	d.Dot = fixed.P((iconPx-w)/2-bounds.Min.X.Floor(), (iconPx-h)/2-bounds.Min.Y.Floor())
	d.DrawString(glyph)
	return img
}
