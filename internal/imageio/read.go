package imageio

import (
	"fmt"
	"image"
	"io"
	"os"
)

// Source is an input file, held open and read-only for the whole conversion.
//
// It is a type rather than a path string because the write path must prove the
// destination is not this file, and only an open descriptor's identity survives a
// symlink, a hard link, or "./a.jpg" versus "a.jpg".
type Source struct {
	f    *os.File
	info os.FileInfo
	path string
}

// Open opens path read-only. os.Open is O_RDONLY, and that is the guarantee that
// this program never writes back to what it read.
func Open(path string) (*Source, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.IsDir() {
		f.Close()
		return nil, fmt.Errorf("%s is a directory", path)
	}
	return &Source{f: f, info: info, path: path}, nil
}

func (s *Source) Close() error { return s.f.Close() }
func (s *Source) Path() string { return s.path }

// Limits bound what a single input may make this program allocate. They are
// checked against the file's HEADER, before any pixel is decoded.
type Limits struct {
	MaxPixels int64 // 0 uses DefaultMaxPixels
	MaxBytes  int64 // 0 uses DefaultMaxBytes
}

const (
	// DefaultMaxPixels is larger than any real photograph and far below what a
	// crafted header can ask for.
	DefaultMaxPixels = 100_000_000
	// MaxAllowedPixels is the ceiling on the limit itself. A knob the operator
	// can open without bound is not a limit, so --max-pixels may raise the floor
	// and never remove the roof.
	MaxAllowedPixels = 1_000_000_000
	// DefaultMaxBytes bounds the decoded buffer. Pixels alone do not: what gets
	// allocated is 4 bytes each, before any transform makes a second buffer.
	DefaultMaxBytes = 512 << 20 // 512 MiB
)

func (l Limits) resolve() (pixels, bytes int64) {
	pixels, bytes = l.MaxPixels, l.MaxBytes
	if pixels <= 0 {
		pixels = DefaultMaxPixels
	}
	if bytes <= 0 {
		bytes = DefaultMaxBytes
	}
	return pixels, bytes
}

// Validate rejects a Limits the operator asked for but that would not be a limit.
func (l Limits) Validate() error {
	if l.MaxPixels < 0 {
		return fmt.Errorf("max-pixels must be positive, got %d", l.MaxPixels)
	}
	if l.MaxPixels > MaxAllowedPixels {
		return fmt.Errorf("max-pixels %d is above the ceiling of %d", l.MaxPixels, MaxAllowedPixels)
	}
	return nil
}

// Sniff reports the format the bytes match, reading only the header. It is how a
// caller asks "is this an image at all" without paying for a decode — a directory
// run has to ask that of every file it finds.
func (s *Source) Sniff() (string, error) {
	if _, err := s.f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	_, format, err := image.DecodeConfig(s.f)
	if err != nil {
		return "", fmt.Errorf("%s: %w", s.path, err)
	}
	return format, nil
}

// Decode reads the image and reports the format its BYTES matched, never its
// extension. The header is checked against limits before anything is decoded,
// which is the whole defence against a few kilobytes that ask for gigabytes.
func (s *Source) Decode(limits Limits) (image.Image, string, error) {
	maxPixels, maxBytes := limits.resolve()

	if _, err := s.f.Seek(0, io.SeekStart); err != nil {
		return nil, "", err
	}
	cfg, format, err := image.DecodeConfig(s.f)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", s.path, err)
	}

	// int64 throughout: 60000*60000 overflows a 32-bit int, and an overflowed
	// product is a small number that passes the check it was meant to fail.
	pixels := int64(cfg.Width) * int64(cfg.Height)
	if pixels > maxPixels {
		return nil, "", fmt.Errorf("%s: %dx%d is %d pixels, above the limit of %d (raise it with --max-pixels)",
			s.path, cfg.Width, cfg.Height, pixels, maxPixels)
	}
	if bytes := pixels * 4; bytes > maxBytes {
		return nil, "", fmt.Errorf("%s: %dx%d would allocate about %d bytes, above the limit of %d",
			s.path, cfg.Width, cfg.Height, bytes, maxBytes)
	}

	if _, err := s.f.Seek(0, io.SeekStart); err != nil {
		return nil, "", err
	}
	img, format, err := image.Decode(s.f)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", s.path, err)
	}
	return img, format, nil
}
