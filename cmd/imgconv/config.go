package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mateusands/imgconv/internal/convert"
	"github.com/mateusands/imgconv/internal/imageio"
	"github.com/mateusands/imgconv/internal/pipeline"
)

// usageError marks an invocation that was wrong in itself — a bad flag, a missing
// argument, two flags that contradict each other. It is the only thing that exits
// 2, and .crew/operations.md makes that a contract a shell script may branch on.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

func usagef(format string, a ...any) error {
	return usageError{err: fmt.Errorf(format, a...)}
}

// exitCode maps an outcome onto the published codes: 0 success, 1 a conversion
// that failed, 2 an invocation that was wrong.
func exitCode(err error) int {
	var u usageError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &u):
		return 2
	default:
		return 1
	}
}

// config is one fully validated invocation. Every rule has already been applied
// by the time this exists, so nothing below it re-reads a flag or re-decides.
type config struct {
	// tui is set only when there were no arguments at all.
	tui bool
	// ui is set by --ui: the graphical front end, served to the browser.
	// uiCeiling is the highest directory it may ever reach; uiStart is where the
	// page opens, and is always inside it.
	ui        bool
	uiCeiling string
	uiStart   string
	// dir says the input is a directory, which is a batch run into outputDir.
	// output is empty then, and outputDir is empty for a single file.
	dir       bool
	input     string
	output    string
	outputDir string
	target    imageio.Format
	codec     imageio.Options
	transform convert.Options
	limits    imageio.Limits
	force     bool
	// jobs is zero when --jobs was not given, which batch reads as one per CPU.
	jobs int
}

// flags are the values exactly as typed, before any rule has been applied.
type flags struct {
	output    string
	to        string
	outdir    string
	resize    string
	force     bool
	quality   int
	jobs      int
	maxPixels int64
	ui        bool
}

// newFlagSet declares the flags. It exists so that the help text and the parser
// read from one declaration — a second list of flags in the help is a list that
// goes stale.
func newFlagSet() (*flag.FlagSet, *flags) {
	fs := flag.NewFlagSet("imgconv", flag.ContinueOnError)
	// The flag package's own error output would arrive without the usage text and
	// without the "imgconv:" prefix; run prints both.
	fs.SetOutput(io.Discard)

	var f flags
	fs.StringVar(&f.output, "o", "", "write the converted image to this path; its extension picks the target format")
	fs.StringVar(&f.to, "to", "", "target format by name, e.g. png")
	fs.StringVar(&f.outdir, "outdir", "", "directory to write a directory run into (required for one)")
	fs.BoolVar(&f.force, "force", false, "replace an existing output file")
	fs.IntVar(&f.quality, "quality", 0, "JPEG quality, 1-100; only for a jpeg target")
	fs.StringVar(&f.resize, "resize", "", "WxH, or Wx / xH to keep the aspect ratio")
	fs.IntVar(&f.jobs, "jobs", 0, "conversions in flight at once during a directory run (default one per CPU)")
	fs.Int64Var(&f.maxPixels, "max-pixels", 0, "refuse an input whose header declares more pixels than this")
	fs.BoolVar(&f.ui, "ui", false, "open the graphical interface in a browser, for the current directory or the one given")
	return fs, &f
}

// parse turns the argument slice into a validated config. It reads no global
// state and exits no process, which is what makes every rule below testable
// without building and running a binary.
//
// A usageError means the invocation was wrong; any other error means the request
// was well formed and could not be served, which the exit-code contract counts as
// a conversion failure.
func parse(args []string) (config, error) {
	fs, f := newFlagSet()

	// The standard flag package stops at the first non-flag argument, and the
	// contract puts the input path FIRST (`imgconv photo.jpg --to png`). Parsing
	// in a loop — take a positional, parse what follows it — is what lets flags
	// appear on either side of the input.
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return config{}, err
			}
			return config{}, usagef("%w", err)
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}

	given := map[string]bool{}
	fs.Visit(func(fl *flag.Flag) { given[fl.Name] = true })

	switch {
	case f.ui:
		// --ui is checked before every other rule because none of the others apply:
		// the interface asks for its own target and its own options later.
		//
		// Given a directory, that directory is the ceiling AND where the page
		// opens. Given none, the ceiling is the operator's home and the page opens
		// where they are standing — so browsing is useful out of the box without
		// the tool being able to reach /etc or another account. `--ui /` is the
		// explicit way to say "everything".
		if len(positional) > 1 {
			return config{}, usagef("--ui takes at most one directory, got %d", len(positional))
		}
		if len(positional) == 1 {
			return config{ui: true, uiCeiling: positional[0], uiStart: positional[0]}, nil
		}
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			home = "."
		}
		return config{ui: true, uiCeiling: home, uiStart: "."}, nil

	case len(positional) == 0 && len(given) == 0:
		return config{tui: true}, nil
	case len(positional) == 0:
		return config{}, usagef("no input path given; run imgconv with no arguments at all for the interactive front end")
	case len(positional) > 1:
		return config{}, usagef("one input path at a time, got %d: %s", len(positional), strings.Join(positional, " "))
	}

	target, err := resolveTarget(given, f)
	if err != nil {
		return config{}, err
	}

	cfg := config{
		input:  positional[0],
		target: target,
		force:  f.force,
		limits: imageio.Limits{MaxPixels: f.maxPixels},
	}
	// Validate's message already names max-pixels, so it is wrapped and not
	// re-worded — one wording, in the package that owns the ceiling.
	if err := cfg.limits.Validate(); err != nil {
		return config{}, usagef("%w", err)
	}

	if given["resize"] {
		if cfg.transform, err = parseResize(f.resize); err != nil {
			return config{}, err
		}
	}
	if given["quality"] {
		if f.quality < 1 || f.quality > 100 {
			return config{}, usagef("--quality must be between 1 and 100, got %d", f.quality)
		}
		if target.Name != "jpeg" {
			return config{}, usagef("--quality is a JPEG encoder setting and the target is %s; it would be ignored", target.Name)
		}
		cfg.codec.Quality = f.quality
	}
	if given["jobs"] {
		if f.jobs < 1 {
			return config{}, usagef("--jobs must be at least 1, got %d", f.jobs)
		}
		cfg.jobs = f.jobs
	}

	info, err := os.Stat(cfg.input)
	if err != nil {
		return config{}, err
	}

	if info.IsDir() {
		if given["o"] {
			return config{}, usagef("-o names one output file and %s is a directory; use --to with --outdir", cfg.input)
		}
		if !given["outdir"] {
			return config{}, usagef("converting the directory %s needs --outdir to say where the results go", cfg.input)
		}
		cfg.dir = true
		cfg.outputDir = f.outdir
		return cfg, nil
	}

	// --outdir on a single file is refused rather than ignored: ignoring it would
	// write the result somewhere the operator did not name, next to the original.
	// --jobs is ignored here instead of refused, because it changes nothing about
	// what gets written.
	if given["outdir"] {
		return config{}, usagef("--outdir applies to a directory run; name the output of a single file with -o")
	}
	if given["o"] {
		cfg.output = f.output
	} else if cfg.output, err = besideInput(cfg.input, target); err != nil {
		return config{}, err
	}
	return cfg, nil
}

// resolveTarget settles which format the output will be encoded in.
//
// One of -o and --to is required and neither is guessed: `imgconv photo.jpg` on
// its own could mean four different things, and picking one writes a file nobody
// asked for. Given both, they must agree — `-o out.png --to jpeg` is a
// contradiction, not a precedence puzzle.
func resolveTarget(given map[string]bool, f *flags) (imageio.Format, error) {
	var target imageio.Format

	switch {
	case !given["o"] && !given["to"]:
		return target, usagef("one of -o or --to is required: imgconv <input> -o <output>, or imgconv <input> --to png")

	case given["to"]:
		named, ok := imageio.Lookup(f.to)
		if !ok {
			return target, fmt.Errorf("%q is not a format this build knows; targets are: %s", f.to, encodableNames())
		}
		target = named
	}

	if given["o"] {
		ext := filepath.Ext(f.output)
		byExt, ok := imageio.ByExtension(ext)
		if !ok {
			return target, fmt.Errorf("cannot tell the target format from %q; name it with --to, or use an extension of: %s",
				f.output, encodableNames())
		}
		if given["to"] && byExt.Name != target.Name {
			return target, usagef("-o %s asks for %s and --to asks for %s; they must agree", f.output, byExt.Name, target.Name)
		}
		target = byExt
	}

	if !target.CanEncode() {
		return target, fmt.Errorf("%s: decode only, cannot be a target format", target.Name)
	}
	return target, nil
}

// besideInput is the single-file output name: same basename, the target's
// extension, in the input's own directory. The naming itself is pipeline's, so
// that the CLI, the TUI and a directory run cannot disagree about where a
// conversion lands.
func besideInput(input string, target imageio.Format) (string, error) {
	if len(target.Extensions) == 0 {
		return "", fmt.Errorf("%s has no extension to name an output with; use -o", target.Name)
	}
	return pipeline.OutputPath(input, "", target), nil
}

// parseResize reads WxH, Wx or xH. An omitted side is zero, which is how convert
// is asked to derive it from the aspect ratio; a side that is present must be a
// positive number, because a zero-pixel dimension is a mistake and not a request.
func parseResize(s string) (convert.Options, error) {
	w, h, found := strings.Cut(s, "x")
	if !found || (w == "" && h == "") {
		return convert.Options{}, usagef("--resize wants WxH, Wx or xH, got %q", s)
	}

	side := func(text, name string) (int, error) {
		if text == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(text)
		if err != nil {
			return 0, usagef("--resize %s: %s is not a number", s, name)
		}
		if n < 1 {
			return 0, usagef("--resize %s: %s must be at least 1, got %d", s, name, n)
		}
		return n, nil
	}

	width, err := side(w, "width")
	if err != nil {
		return convert.Options{}, err
	}
	height, err := side(h, "height")
	if err != nil {
		return convert.Options{}, err
	}
	return convert.Options{Width: width, Height: height}, nil
}

// encodableNames lists the formats that may be a target, read from the registry
// so that adding a row to it updates every message here.
func encodableNames() string {
	var names []string
	for _, f := range imageio.Formats() {
		if f.CanEncode() {
			names = append(names, f.Name)
		}
	}
	return strings.Join(names, ", ")
}
