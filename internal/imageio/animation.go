package imageio

import (
	"bufio"
	"fmt"
	"io"
)

// IsAnimated reports whether the input holds more than one frame. Of the formats
// in the registry only GIF can, so everything else answers false without reading.
//
// The caller warns when this is true and the target format is a still one: the
// frames after the first are dropped, and dropping them silently is the failure
// this exists to prevent. An error here is not a reason to fail the conversion —
// a caller that cannot answer the question should convert and not warn.
func (s *Source) IsAnimated(format string) (bool, error) {
	if format != "gif" {
		return false, nil
	}
	if _, err := s.f.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	// Stop at two: the question is "more than one", not "how many".
	frames, err := countGIFFrames(bufio.NewReader(s.f), 2)
	if err != nil {
		return false, err
	}
	return frames > 1, nil
}

// countGIFFrames counts image descriptors, stopping once it has seen stopAt.
//
// It never decompresses pixel data — sub-blocks are walked by their length bytes
// and thrown away. gif.DecodeAll would answer the same question by allocating
// every frame of a file this program does not trust, which is the decode bomb the
// read path already refuses; a file declaring thousands of frames costs its own
// size in reads here and nothing per frame.
func countGIFFrames(r *bufio.Reader, stopAt int) (int, error) {
	header := make([]byte, 6)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, err
	}
	if string(header[:3]) != "GIF" {
		return 0, fmt.Errorf("not a GIF: header begins %q", header[:3])
	}

	// Logical screen descriptor. Only the packed byte matters here: bit 7 says a
	// global colour table follows it, and bits 0-2 give that table's size.
	lsd := make([]byte, 7)
	if _, err := io.ReadFull(r, lsd); err != nil {
		return 0, err
	}
	if lsd[4]&0x80 != 0 {
		if err := discard(r, colorTableBytes(lsd[4])); err != nil {
			return 0, err
		}
	}

	frames := 0
	for {
		introducer, err := r.ReadByte()
		if err == io.EOF {
			// Truncated. Report what was actually seen rather than failing: a
			// warning is not worth refusing a file the decoder may still read.
			return frames, nil
		}
		if err != nil {
			return 0, err
		}

		switch introducer {
		case gifTrailer:
			return frames, nil

		case gifExtension:
			if _, err := r.ReadByte(); err != nil { // the extension label
				return 0, err
			}
			if err := skipSubBlocks(r); err != nil {
				return 0, err
			}

		case gifImageDescriptor:
			frames++
			if frames >= stopAt {
				return frames, nil
			}
			desc := make([]byte, 9)
			if _, err := io.ReadFull(r, desc); err != nil {
				return 0, err
			}
			if desc[8]&0x80 != 0 { // a local colour table follows
				if err := discard(r, colorTableBytes(desc[8])); err != nil {
					return 0, err
				}
			}
			if _, err := r.ReadByte(); err != nil { // LZW minimum code size
				return 0, err
			}
			if err := skipSubBlocks(r); err != nil {
				return 0, err
			}

		default:
			return 0, fmt.Errorf("unexpected GIF block introducer 0x%02X", introducer)
		}
	}
}

const (
	gifExtension       = 0x21
	gifImageDescriptor = 0x2C
	gifTrailer         = 0x3B
)

// colorTableBytes decodes the table size packed into bits 0-2: the spec stores n
// and means 3 * 2^(n+1) bytes, three per entry.
func colorTableBytes(packed byte) int64 { return 3 << ((packed & 0x07) + 1) }

// skipSubBlocks walks a chain of length-prefixed sub-blocks to its terminator.
func skipSubBlocks(r *bufio.Reader) error {
	for {
		n, err := r.ReadByte()
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		if err := discard(r, int64(n)); err != nil {
			return err
		}
	}
}

func discard(r *bufio.Reader, n int64) error {
	_, err := io.CopyN(io.Discard, r, n)
	return err
}
