// Package pipeline is the one place a single file is converted.
//
// It exists because the same three steps — decode, transform, encode — were
// written three times: once in the CLI, once in the TUI and once inside the
// directory run. They had already drifted. Only two of them warned that an
// animated GIF was being flattened, so converting a folder lost every frame but
// the first without saying a word, while converting the same file on its own
// said so. AGENTS.md calls a behaviour that differs between the front ends a bug,
// and three copies of one decision is how that bug is born.
//
// Everything above this — cmd, tui, batch — decides WHICH files and WHERE the
// output goes. What happens to one file happens here, once.
package pipeline

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mateusands/imgconv/internal/convert"
	"github.com/mateusands/imgconv/internal/imageio"
)

// Request is one file's conversion, fully decided. Nothing here is guessed.
type Request struct {
	Input     string
	Output    string
	Target    imageio.Format
	Codec     imageio.Options
	Transform convert.Options
	Limits    imageio.Limits
	Force     bool
}

// Outcome is what the caller has to tell the operator even when nothing failed.
// Warning is empty when there is nothing to say.
type Outcome struct {
	// Format is what the input's BYTES matched, never what its name claimed.
	Format  string
	Warning string
}

// Convert runs decode, transform and encode for one file.
//
// The Source stays open across all three steps because the write path settles
// the destination's identity against it — that is what refuses to write over the
// operator's own input, and it needs an open descriptor rather than a path.
func Convert(req Request) (Outcome, error) {
	src, err := imageio.Open(req.Input)
	if err != nil {
		return Outcome{}, err
	}
	defer src.Close()

	img, format, err := src.Decode(req.Limits)
	if err != nil {
		return Outcome{}, err
	}
	out := Outcome{Format: format}

	// The error is dropped deliberately: IsAnimated's contract is that a caller
	// which cannot answer the question converts without the warning. Losing the
	// warning is a smaller harm than refusing a file the decoder can read.
	if animated, _ := src.IsAnimated(format); animated {
		out.Warning = fmt.Sprintf("%s holds more than one frame and %s does not: only the first frame was written",
			filepath.Base(req.Input), req.Target.Name)
	}

	// One ceiling, one knob. The operator raises --max-pixels to accept a bigger
	// input; it would be a trap for that to leave the OUTPUT bounded by something
	// else, so the same number governs both allocations.
	transform := req.Transform
	if transform.MaxPixels == 0 {
		transform.MaxPixels = req.Limits.MaxPixels
	}
	img, err = convert.Resize(img, transform)
	if err != nil {
		return out, err
	}
	if err := imageio.Write(req.Output, src, img, req.Target, req.Codec, req.Force); err != nil {
		return out, err
	}
	return out, nil
}

// OutputPath names where converting input to f writes: the same basename with
// the target's first extension. An empty dir means beside the input.
//
// The result can name the input itself — choosing png for a .png — and that
// refusal belongs to imageio.Write, which settles identity on open descriptors.
// Deciding it here would be a second, weaker guard.
func OutputPath(input, dir string, f imageio.Format) string {
	ext := ""
	if len(f.Extensions) > 0 {
		ext = f.Extensions[0]
	}
	base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input)) + ext
	if dir == "" {
		dir = filepath.Dir(input)
	}
	return filepath.Join(dir, base)
}
