// Package imageio is the only package here that knows what an image FORMAT is.
// Everything above it works with an image.Image and never learns where the pixels
// came from — that is what lets resizing need no per-format code.
package imageio

import (
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"strings"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
	// Registers the WebP sniffer with image.Decode. Blank because we only ever
	// read WebP: x/image ships a decoder and no encoder.
	_ "golang.org/x/image/webp"
)

// Options are what an encoder needs, and nothing else. Width and height belong to
// convert.Options; splitting them is what keeps imageio from importing upward.
type Options struct {
	// Quality is the JPEG quality, 1–100. Zero means the encoder's own default.
	// Every other format ignores it.
	Quality int
}

// Format is one row of the registry. A nil Encode means decode-only: a real case
// (WebP), not an oversight, and the reason this is a table and not a switch.
type Format struct {
	Name       string
	Extensions []string
	Encode     func(io.Writer, image.Image, Options) error
}

// CanEncode reports whether this format may be a conversion TARGET.
func (f Format) CanEncode() bool { return f.Encode != nil }

// registry is the single source of truth for supported formats: the CLI help, the
// TUI list and the extension guess all read it. A switch on format name outside
// this package means the abstraction leaked.
//
// Decoding needs no entry — the blank imports above register each sniffer with the
// standard library. Extensions here only ever pick a TARGET.
var registry = []Format{
	{Name: "jpeg", Extensions: []string{".jpg", ".jpeg"}, Encode: encodeJPEG},
	{Name: "png", Extensions: []string{".png"}, Encode: encodePNG},
	{Name: "gif", Extensions: []string{".gif"}, Encode: encodeGIF},
	{Name: "tiff", Extensions: []string{".tif", ".tiff"}, Encode: encodeTIFF},
	{Name: "bmp", Extensions: []string{".bmp"}, Encode: encodeBMP},
	{Name: "webp", Extensions: []string{".webp"}, Encode: nil},
}

func encodeJPEG(w io.Writer, img image.Image, o Options) error {
	q := o.Quality
	if q == 0 {
		q = jpeg.DefaultQuality
	}
	return jpeg.Encode(w, img, &jpeg.Options{Quality: q})
}

func encodePNG(w io.Writer, img image.Image, _ Options) error  { return png.Encode(w, img) }
func encodeGIF(w io.Writer, img image.Image, _ Options) error  { return gif.Encode(w, img, nil) }
func encodeTIFF(w io.Writer, img image.Image, _ Options) error { return tiff.Encode(w, img, nil) }
func encodeBMP(w io.Writer, img image.Image, _ Options) error  { return bmp.Encode(w, img) }

// Lookup finds a format by the name image.Decode reports, e.g. "jpeg".
func Lookup(name string) (Format, bool) {
	for _, f := range registry {
		if f.Name == strings.ToLower(name) {
			return f, true
		}
	}
	return Format{}, false
}

// ByExtension finds the format that answers to a file extension, with or without
// the leading dot. It is for choosing a TARGET only — an input's format comes
// from its bytes, never from its name.
func ByExtension(ext string) (Format, bool) {
	ext = strings.ToLower(ext)
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	for _, f := range registry {
		for _, e := range f.Extensions {
			if e == ext {
				return f, true
			}
		}
	}
	return Format{}, false
}

// Formats returns a copy of the registry. The copy is deliberate: a caller that
// could append to it would become a second source.
func Formats() []Format {
	out := make([]Format, len(registry))
	copy(out, registry)
	return out
}
