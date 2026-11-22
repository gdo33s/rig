package kitty

import (
	"bytes"
	"image"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSendChunksAndPlaces(t *testing.T) {
	var buf bytes.Buffer
	// Noise does not compress, so it needs several chunks.
	img := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	r := rand.New(rand.NewPCG(1, 2))
	for i := range img.Pix {
		img.Pix[i] = byte(r.Uint32())
	}
	if err := Send(&buf, 3, img, 4, 2); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "\x1b_Ga=t,f=100,t=d,i=3,q=2,m=1;") {
		t.Fatalf("first chunk = %q", out[:40])
	}
	if !strings.Contains(out, "\x1b_Gm=0;") {
		t.Fatal("no final chunk")
	}
	if !strings.HasSuffix(out, "\x1b_Ga=p,U=1,i=3,c=4,r=2,q=2\x1b\\") {
		t.Fatalf("placement = %q", out[len(out)-40:])
	}
	for _, part := range strings.Split(out, "\x1b\\") {
		if _, payload, ok := strings.Cut(part, ";"); ok && len(payload) > chunk {
			t.Fatalf("chunk of %d bytes", len(payload))
		}
	}
}

func TestRowIsCellsWide(t *testing.T) {
	row := Row(2, 1, 4)
	if w := ansi.StringWidth(row); w != 4 {
		t.Fatalf("width = %d, want 4", w)
	}
	if !strings.HasPrefix(row, "\x1b[38;5;2m\U0010EEEE̍̅") {
		t.Fatalf("row = %q", row)
	}
}
