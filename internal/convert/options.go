package convert

import "fmt"

// Options are the transform options: the dimensions asked of a resize. Zero
// means "not given" for either side, and both zero means no resize at all.
//
// There is deliberately no Quality here — quality is a codec setting, so it
// belongs to imageio.Options, and the rule that only a JPEG target accepts it is
// decidable only where the target format is known, which is cmd.
type Options struct {
	Width  int
	Height int

	// MaxPixels caps the OUTPUT, which is a different question from capping the
	// input: the operator names one side and the other is derived from the source's
	// aspect ratio, so a tiny file with an extreme ratio can ask for an enormous
	// result. Zero means DefaultMaxPixels — unset must still be bounded, or every
	// caller that forgets reopens the hole.
	MaxPixels int64
}

// DefaultMaxPixels bounds a resize when the caller names no limit.
//
// It is the same number as imageio.DefaultMaxPixels and deliberately not that
// constant: convert may not import imageio without putting a back-edge in the
// layer arrow, which is the property that keeps this package pure. If one moves,
// move the other.
const DefaultMaxPixels = 100_000_000

// Validate reports why these Options cannot be applied, or nil. Both dimensions
// zero is legal and is how a caller asks for no resize.
func (o Options) Validate() error {
	if o.Width < 0 {
		return fmt.Errorf("width must be zero or positive, got %d", o.Width)
	}
	if o.Height < 0 {
		return fmt.Errorf("height must be zero or positive, got %d", o.Height)
	}
	if o.MaxPixels < 0 {
		return fmt.Errorf("max-pixels must be zero or positive, got %d", o.MaxPixels)
	}
	return nil
}
