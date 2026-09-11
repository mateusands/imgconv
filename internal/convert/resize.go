package convert

import (
	"fmt"
	"image"

	"golang.org/x/image/draw"
)

// Resize scales img to the dimensions in opts and returns the result; img itself
// is never modified. Giving one dimension derives the other from the source's
// aspect ratio; giving both uses both exactly, distortion included; giving
// neither returns img itself, unchanged and uncopied. It returns an error when
// opts is invalid, or when the source has no pixels to scale.
func Resize(img image.Image, opts Options) (image.Image, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	width, height := opts.Width, opts.Height
	if width == 0 && height == 0 {
		return img, nil
	}

	src := img.Bounds()
	// <= rather than ==: image.Rect canonicalises, so a standard decoder never
	// hands over an inverted rectangle — but image.Image is an interface and a
	// caller can implement it, and a negative Dx would reach derive as a negative
	// base and come back out as a corrupted dimension.
	if src.Dx() <= 0 || src.Dy() <= 0 {
		return nil, fmt.Errorf("cannot resize an image with no pixels (%dx%d)", src.Dx(), src.Dy())
	}

	switch {
	case height == 0:
		height = derive(src.Dy(), width, src.Dx())
	case width == 0:
		width = derive(src.Dx(), height, src.Dy())
	}

	// Bound the allocation BEFORE making it. The read path refuses an input whose
	// header declares too many pixels; without this, a 1x100 file asked for
	// --resize 100x still produced 100x10000, and the growth is quadratic.
	// int64 for the same reason it is used on the decode side: the product
	// overflows a 32-bit int, and an overflowed product is a small number that
	// passes the check it was meant to fail.
	maxPixels := opts.MaxPixels
	if maxPixels <= 0 {
		maxPixels = DefaultMaxPixels
	}
	if pixels := int64(width) * int64(height); pixels > maxPixels {
		return nil, fmt.Errorf("resizing to %dx%d is %d pixels, above the limit of %d", width, height, pixels, maxPixels)
	}

	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	// draw.Src, not draw.Over: dst is a fresh buffer, so compositing would only
	// blend the result against transparent black and lose the source's own alpha.
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, src, draw.Src, nil)
	return dst, nil
}

// derive returns side scaled by target/base, rounded to nearest, never below 1.
//
// int64 for the multiply: side*target overflows a 32-bit int on a large image,
// and an overflowed product is a small number that looks like a valid dimension.
// The floor of 1 is a bound the caller cannot see — a very wide, very short image
// scaled down derives a height of zero, and a zero-sided image is not a result.
func derive(side, target, base int) int {
	scaled := (int64(side)*int64(target) + int64(base)/2) / int64(base)
	if scaled < 1 {
		return 1
	}
	return int(scaled)
}
