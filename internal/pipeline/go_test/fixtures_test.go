package go_test

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"
)

// animatedGIFBytes encodes n real frames with the standard library, so a fixture
// the tests call animated is one the stdlib itself agrees is animated.
func animatedGIFBytes(t *testing.T, n int) []byte {
	t.Helper()
	g := &gif.GIF{}
	for i := 0; i < n; i++ {
		p := image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{
			color.RGBA{A: 255}, color.RGBA{R: uint8(i * 40), A: 255},
		})
		p.SetColorIndex(i%4, 0, 1)
		g.Image = append(g.Image, p)
		g.Delay = append(g.Delay, 10)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatalf("building the %d-frame fixture: %v", n, err)
	}
	return buf.Bytes()
}
