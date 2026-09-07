// Package batch converts every image in one directory, with a bounded number of
// conversions in flight at once.
//
// It is the layer between cmd and convert/imageio: it decides WHICH files take
// part and how many run at a time, and it decides nothing about image formats —
// that stays in imageio, which is the only package allowed to know.
package batch

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/mateusands/imgconv/internal/convert"
	"github.com/mateusands/imgconv/internal/imageio"
	"github.com/mateusands/imgconv/internal/pipeline"
)

// Request is one directory run. Jobs zero means runtime.NumCPU(); the caller
// distinguishes "not asked" from "asked for none", and asking for none is an error.
type Request struct {
	InputDir  string
	OutputDir string
	Target    imageio.Format
	Codec     imageio.Options
	Transform convert.Options
	Limits    imageio.Limits
	Force     bool
	Jobs      int
}

// Result is what happened to one input. A nil Err means Output was written.
//
// Warning is set when the conversion succeeded but cost something the operator
// has to hear about — flattening an animated GIF is the case that exists today.
// It is carried per file rather than printed here because this package has no
// output of its own: the front end decides where a message goes.
type Result struct {
	Input   string
	Output  string
	Warning string
	Err     error
}

// Summary accounts for every file the run took part in, sorted by input path so
// that two identical invocations produce output that can be diffed.
type Summary struct {
	Results []Result
}

// Failures returns the results that did not convert. The caller exits non-zero
// when this is non-empty; a batch that hides one failure is the defect this
// package exists to avoid.
func (s Summary) Failures() []Result {
	var failed []Result
	for _, r := range s.Results {
		if r.Err != nil {
			failed = append(failed, r)
		}
	}
	return failed
}

// Converted counts the inputs that produced an output file.
func (s Summary) Converted() int {
	return len(s.Results) - len(s.Failures())
}

// unit is one input paired with the output path it was planned onto.
type unit struct {
	input  string
	output string
}

// Run converts every image in req.InputDir into req.OutputDir.
//
// It returns an error only when the RUN is refused as a whole — bad arguments, an
// unreadable directory, or two inputs planned onto one output. A file that fails
// on its own is not an error here: it is a Result with an Err, so that the other
// files still convert. That split is the contract.
func Run(req Request) (Summary, error) {
	jobs := req.Jobs
	switch {
	case jobs < 0:
		return Summary{}, fmt.Errorf("jobs must be at least 1, got %d", jobs)
	case jobs == 0:
		jobs = runtime.NumCPU()
	}
	if !req.Target.CanEncode() {
		return Summary{}, fmt.Errorf("%s: decode only, cannot be a target format", req.Target.Name)
	}
	if err := req.Transform.Validate(); err != nil {
		return Summary{}, err
	}
	if err := req.Limits.Validate(); err != nil {
		return Summary{}, err
	}

	units, err := plan(req)
	if err != nil {
		return Summary{}, err
	}

	results := make([]Result, len(units))
	work := make(chan int)

	var wg sync.WaitGroup
	for w := 0; w < jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each worker writes results[i] for an index no other worker receives,
			// so distinct elements of one slice are written concurrently without a
			// mutex. That is safe in Go; sharing the slice HEADER would not be, and
			// nothing here appends.
			for i := range work {
				warning, err := convertOne(units[i], req)
				results[i] = Result{
					Input:   units[i].input,
					Output:  units[i].output,
					Warning: warning,
					Err:     err,
				}
			}
		}()
	}
	for i := range units {
		work <- i
	}
	close(work)
	wg.Wait()

	return Summary{Results: results}, nil
}

// plan decides which files take part and where each one lands, and refuses the
// whole run if two of them would land on the same name.
//
// The refusal happens here, before a single conversion, on purpose: converting
// one file and then overwriting it with another is the silent data loss this tool
// exists not to do, and --force does not lift it because there is no "intended"
// winner between two inputs the operator named equally.
func plan(req Request) ([]unit, error) {
	entries, err := os.ReadDir(req.InputDir)
	if err != nil {
		return nil, err
	}
	if len(req.Target.Extensions) == 0 {
		return nil, fmt.Errorf("%s has no extension to name an output with", req.Target.Name)
	}

	var units []unit
	collisions := map[string][]string{}
	for _, e := range entries {
		// One level. Recursing is out of scope for v1 and doing it silently would
		// convert files the operator never looked at.
		if e.IsDir() || !e.Type().IsRegular() {
			continue
		}
		input := filepath.Join(req.InputDir, e.Name())
		if !participates(input) {
			continue
		}
		output := pipeline.OutputPath(e.Name(), req.OutputDir, req.Target)
		units = append(units, unit{input: input, output: output})
		collisions[output] = append(collisions[output], e.Name())
	}

	var clashes []string
	for output, inputs := range collisions {
		if len(inputs) > 1 {
			sort.Strings(inputs)
			clashes = append(clashes, fmt.Sprintf("%s <- %s", filepath.Base(output), strings.Join(inputs, ", ")))
		}
	}
	if len(clashes) > 0 {
		sort.Strings(clashes)
		return nil, fmt.Errorf("refusing the run: %d output name(s) would be written twice (--force does not choose a winner): %s",
			len(clashes), strings.Join(clashes, "; "))
	}

	sort.Slice(units, func(i, j int) bool { return units[i].input < units[j].input })
	return units, nil
}

// participates reports whether a file takes part in the run.
//
// The extension is NOT the filter, and the asymmetry is deliberate. A name that
// claims an image format buys the file a conversion attempt, so a .jpg holding
// text is REPORTED as a failure rather than skipped — the operator meant it to be
// an image and needs to hear that it is not. A name that claims nothing has to
// earn its place with its bytes, so a .txt holding a PNG is converted and a .txt
// holding text is passed over in silence.
func participates(path string) bool {
	if _, named := imageio.ByExtension(filepath.Ext(path)); named {
		return true
	}
	src, err := imageio.Open(path)
	if err != nil {
		return false
	}
	defer src.Close()
	_, err = src.Sniff()
	return err == nil
}

// convertOne converts a single file through the shared pipeline, and returns the
// warning it produced. There is deliberately no conversion code here: the three
// steps live in one place so that a directory run and a single-file run cannot
// drift apart, which they already had once.
func convertOne(u unit, req Request) (string, error) {
	out, err := pipeline.Convert(pipeline.Request{
		Input:     u.input,
		Output:    u.output,
		Target:    req.Target,
		Codec:     req.Codec,
		Transform: req.Transform,
		Limits:    req.Limits,
		Force:     req.Force,
	})
	return out.Warning, err
}
