package go_test

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"testing"
)

func writePNGAt(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 20), B: 140, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func animatedGIFAt(t *testing.T, n int) []byte {
	t.Helper()
	g := &gif.GIF{}
	for i := 0; i < n; i++ {
		p := image.NewPaletted(image.Rect(0, 0, 8, 8), color.Palette{
			color.RGBA{A: 255}, color.RGBA{R: uint8(i * 50), A: 255},
		})
		p.SetColorIndex(i%8, 0, 1)
		g.Image = append(g.Image, p)
		g.Delay = append(g.Delay, 10)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
