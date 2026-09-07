// Command imgconv converts image files between formats, one file or a whole
// directory, from flags or from an interactive terminal front end.
//
// This package parses flags, prints results and chooses an exit code. It contains
// no image logic: the layers below it decide what a format is (imageio), what a
// transform does (convert) and how a directory run is scheduled (batch).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mateusands/imgconv/internal/batch"
	"github.com/mateusands/imgconv/internal/imageio"
	"github.com/mateusands/imgconv/internal/pipeline"
	"github.com/mateusands/imgconv/internal/tui"
	"github.com/mateusands/imgconv/internal/web"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main with its process boundaries passed in, so that the exit code and
// everything written to each stream can be asserted in a test.
func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parse(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, usageText())
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "imgconv: %v\n", err)
		if exitCode(err) == 2 {
			fmt.Fprint(stderr, usageText())
		}
		return exitCode(err)
	}

	switch {
	case cfg.ui:
		err = web.Run(cfg.uiStart, stdout)
	case cfg.tui:
		err = tui.Run()
	case cfg.dir:
		err = convertDirectory(cfg, stdout, stderr)
	default:
		err = convertFile(cfg, stdout, stderr)
	}
	if err != nil {
		fmt.Fprintf(stderr, "imgconv: %v\n", err)
	}
	return exitCode(err)
}

// convertFile converts one file through the shared pipeline and reports it.
//
// There is deliberately no conversion code here. The three steps live in
// internal/pipeline so that this path, the TUI's and a directory run cannot drift
// apart — they already had, and the CLI was one of the two that warned about a
// flattened GIF while the directory run said nothing.
func convertFile(cfg config, stdout, stderr io.Writer) error {
	out, err := pipeline.Convert(pipeline.Request{
		Input:     cfg.input,
		Output:    cfg.output,
		Target:    cfg.target,
		Codec:     cfg.codec,
		Transform: cfg.transform,
		Limits:    cfg.limits,
		Force:     cfg.force,
	})
	// The warning is printed even when the conversion then failed: it describes
	// the input, which is true either way.
	if out.Warning != "" {
		fmt.Fprintf(stderr, "imgconv: %s\n", out.Warning)
	}
	if err != nil {
		return withForceHint(err)
	}
	fmt.Fprintf(stdout, "%s -> %s\n", cfg.input, cfg.output)
	return nil
}

// convertDirectory runs the batch and reports it. A run where some files failed
// still exits non-zero and names every one of them: a partial success reported as
// success is the defect this program's rules exist to prevent.
func convertDirectory(cfg config, stdout, stderr io.Writer) error {
	summary, err := batch.Run(batch.Request{
		InputDir:  cfg.input,
		OutputDir: cfg.outputDir,
		Target:    cfg.target,
		Codec:     cfg.codec,
		Transform: cfg.transform,
		Limits:    cfg.limits,
		Force:     cfg.force,
		Jobs:      cfg.jobs,
	})
	if err != nil {
		return err
	}

	// Warnings before failures: they belong to files that DID convert, and burying
	// them under the error list is how a whole folder loses its frames quietly.
	for _, r := range summary.Results {
		if r.Warning != "" {
			fmt.Fprintf(stderr, "imgconv: %s\n", r.Warning)
		}
	}

	failures := summary.Failures()
	for _, f := range failures {
		// Every failure line starts with the input path — that is what the
		// operator has to go and look at, and what a script greps for. imageio
		// already prefixes most of its errors with that same path, so one copy is
		// trimmed rather than printed twice.
		fmt.Fprintf(stderr, "imgconv: %s: %s\n", f.Input, strings.TrimPrefix(f.Err.Error(), f.Input+": "))
	}
	fmt.Fprintf(stdout, "converted %d of %d file(s) into %s\n", summary.Converted(), len(summary.Results), cfg.outputDir)
	if len(failures) > 0 {
		return fmt.Errorf("%d of %d file(s) failed", len(failures), len(summary.Results))
	}
	return nil
}

// usageText is built from the registry and the flag declarations, so that a new
// format or a new flag shows up here without anyone remembering to add it.
func usageText() string {
	var b strings.Builder
	b.WriteString(`
usage:
  imgconv <input> -o <output>              convert one file; -o's extension picks the format
  imgconv <input> --to png                 convert one file, beside the input
  imgconv <dir> --to png --outdir <dir>    convert every image in a directory, one level deep
  imgconv                                  no arguments: the interactive front end

formats:
`)
	for _, f := range imageio.Formats() {
		role := "source or target"
		if !f.CanEncode() {
			role = "source only"
		}
		fmt.Fprintf(&b, "  %-6s %-12s %s\n", f.Name, strings.Join(f.Extensions, " "), role)
	}

	b.WriteString("\nflags:\n")
	fs, _ := newFlagSet()
	fs.SetOutput(&b)
	fs.PrintDefaults()
	return b.String()
}

// withForceHint appends the flag that lifts the refusal. It lives here and not in
// imageio because "--force" is this front end's vocabulary: the browser front end
// has a checkbox for the same choice and would be lying if it said this.
func withForceHint(err error) error {
	if errors.Is(err, imageio.ErrExists) {
		return fmt.Errorf("%w (pass --force to replace it)", err)
	}
	return err
}
