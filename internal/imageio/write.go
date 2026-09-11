package imageio

import (
	"errors"
	"fmt"
	"image"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrExists reports that the destination is already a file. Callers match it with
// errors.Is to add the instruction that fits their own interface.
var ErrExists = errors.New("already exists")

// Write encodes img to target in format f. It refuses to replace an existing file
// unless force is set, and refuses to write over src's own file even when it is.
//
// src may be nil when the image did not come from a file.
func Write(target string, src *Source, img image.Image, f Format, opts Options, force bool) error {
	if !f.CanEncode() {
		return fmt.Errorf("%s: decode only, cannot be a target format", f.Name)
	}

	// The order below is the safety property. Nothing may truncate until the
	// destination has been proven not to be the source: an earlier draft opened
	// with O_TRUNC when forced and compared afterwards, which empties the
	// operator's file before the comparison runs. O_EXCL is the refusal, and the
	// kernel makes it atomic — os.Stat then write leaves a window in between.
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	switch {
	case err == nil:
		// The destination did not exist and this call created it. A path that is
		// a symlink — even to the input, even dangling — fails O_EXCL with EEXIST
		// and lands in the branch below, so nothing here can alias the source.
		return encodeInto(out, target, img, f, opts)

	case errors.Is(err, fs.ErrExist):
		if !force {
			// The fact, and only the fact. How to override it is a different
			// sentence in every front end — a flag on a command line, a checkbox
			// in a browser — so each one appends its own using ErrExists.
			return fmt.Errorf("%w: %s", ErrExists, target)
		}
		return replace(target, src, img, f, opts)

	default:
		return err
	}
}

// encodeInto writes into a file this program just created, and removes it if the
// encode fails. Deleting is safe precisely because the file is ours: it did not
// exist a moment ago, so nobody can be losing anything.
func encodeInto(out *os.File, target string, img image.Image, f Format, opts Options) error {
	if err := f.Encode(out, img, opts); err != nil {
		out.Close()
		os.Remove(target)
		return err
	}
	// The close matters as much as the encode. A full disk buffers the bytes,
	// lets Encode return nil, and only fails on the flush — which would leave a
	// truncated file that looks like a finished one.
	if err := out.Close(); err != nil {
		os.Remove(target)
		return err
	}
	return nil
}

// replace overwrites a destination that already exists — the --force path.
func replace(target string, src *Source, img image.Image, f Format, opts Options) error {
	// Opened WITHOUT O_TRUNC. The file has to survive until it has been compared
	// with the source; that comparison is the only thing standing between
	// "--force -o photo.jpg photo.jpg" and an unrecoverable loss.
	//
	// A DANGLING SYMLINK reaches here too: the entry exists, so O_EXCL refused it,
	// and opening it follows the link to nothing. There is no file to compare
	// against and nothing of the operator's to lose, so the write proceeds — and
	// os.Rename below replaces the LINK rather than writing through it, which is
	// what keeps that safe.
	perm := os.FileMode(0o644)
	existing, err := os.OpenFile(target, os.O_RDONLY, 0)
	switch {
	case err == nil:
		existingInfo, statErr := existing.Stat()
		existing.Close()
		if statErr != nil {
			return statErr
		}
		if src != nil && os.SameFile(src.info, existingInfo) {
			// Same inode. Catches the identical path, a hard link and a symlink
			// alike — none of which a string comparison of paths can see.
			return fmt.Errorf("refusing to write over the input file %s", src.path)
		}
		// Replacing a file must not widen who can read it: a destination the
		// operator kept at 0600 comes back at 0600.
		perm = existingInfo.Mode().Perm()

	case errors.Is(err, fs.ErrNotExist):
		// A dangling symlink. Nothing to compare, nothing to lose.

	default:
		return err
	}

	// The destination is a real file the operator already had, so it must not be
	// damaged by an encode that fails halfway. Encode into a temporary file in
	// the same directory, then rename over it only once the encode is clean.
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".imgconv-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := f.Encode(tmp, img, opts); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	// CreateTemp makes the file 0600. The destination's own permissions are what
	// the result should have — 0644 only when there was no destination to ask.
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
