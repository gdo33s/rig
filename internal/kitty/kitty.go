// Package kitty shows images with the kitty graphics protocol, placed with
// Unicode placeholders.
//
// An image is sent to the terminal once and given a virtual placement of a
// fixed number of cells. It then appears wherever its placeholder cells
// are printed. Placeholders are ordinary text to a TUI renderer, so they
// redraw, move and clear like any other text.
package kitty

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"strings"
)

const chunk = 4096

// Send transmits img as image id and places it virtually, to be drawn
// across cols by rows cells wherever its placeholders appear. The terminal
// is asked not to reply.
func Send(w io.Writer, id int, img image.Image, cols, rows int) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	data := base64.StdEncoding.EncodeToString(buf.Bytes())
	var out strings.Builder
	for first := true; first || data != ""; first = false {
		n := min(chunk, len(data))
		more := 0
		if n < len(data) {
			more = 1
		}
		if first {
			fmt.Fprintf(&out, "\x1b_Ga=t,f=100,t=d,i=%d,q=2,m=%d;%s\x1b\\", id, more, data[:n])
		} else {
			fmt.Fprintf(&out, "\x1b_Gm=%d;%s\x1b\\", more, data[:n])
		}
		data = data[n:]
	}
	fmt.Fprintf(&out, "\x1b_Ga=p,U=1,i=%d,c=%d,r=%d,q=2\x1b\\", id, cols, rows)
	_, err := io.WriteString(w, out.String())
	return err
}

// placeholder is the character the terminal replaces with image cells.
const placeholder = "\U0010EEEE"

// diacritics encode a placeholder's row and column, in the protocol's order.
var diacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F,
	0x0346, 0x034A, 0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357,
}

// Row is the text that draws one row of image id, cols cells wide. The
// image's id is carried by the foreground colour, so ids up to 255 fit.
func Row(id, row, cols int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[38;5;%dm", id)
	for col := range cols {
		b.WriteString(placeholder)
		b.WriteRune(diacritics[row])
		b.WriteRune(diacritics[col])
	}
	b.WriteString("\x1b[39m")
	return b.String()
}
