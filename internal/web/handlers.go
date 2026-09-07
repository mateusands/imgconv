package web

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/mateusands/imgconv/internal/convert"
	"github.com/mateusands/imgconv/internal/imageio"
	"github.com/mateusands/imgconv/internal/pipeline"
)

// thumbWidth is what the page shows, not what it converts. Small on purpose: a
// thumbnail is decoded on demand for every file in the directory, so the cost of
// this number is paid once per image per page load.
const thumbWidth = 240

// assets is the whole interface, compiled into the binary. Embedding is what keeps
// the promise that this ships as ONE file with nothing to install.
//
//go:embed assets
var assets embed.FS

type fileInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type formatInfo struct {
	Name string `json:"name"`
	Ext  string `json:"ext"`
}

// listingResponse carries no path at all. The page works in ids and base names:
// it cannot show the operator's directory tree because it is never told it.
type listingResponse struct {
	Files   []fileInfo   `json:"files"`
	Formats []formatInfo `json:"formats"`
}

type convertRequest struct {
	// Files are ids from the selection, never paths. An id that was not picked
	// resolves to nothing, whatever it is spelled like.
	Files   []string `json:"files"`
	Format  string   `json:"format"`
	Quality int      `json:"quality"`
	Width   int      `json:"width"`
	Height  int      `json:"height"`
	Force   bool     `json:"force"`
}

type convertResult struct {
	Input   string `json:"input"`
	Output  string `json:"output,omitempty"`
	Warning string `json:"warning,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/files", s.handleFiles)
	s.mux.HandleFunc("/api/thumb", s.handleThumb)
	s.mux.HandleFunc("/api/convert", s.handleConvert)
	s.mux.HandleFunc("/api/pick", s.handlePick)
	s.mux.HandleFunc("/api/remove", s.handleRemove)
	s.mux.HandleFunc("/api/quit", s.handleQuit)
	s.mux.HandleFunc("/", s.handlePage)
}

// handleFiles lists what can be converted and what it can be converted to.
//
// Both lists come from the same places the CLI and the TUI read: the bytes decide
// what is an image, and imageio's registry decides what a target may be. A list
// written out here would be a second source and would go stale.
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	out := listingResponse{Files: []fileInfo{}, Formats: []formatInfo{}}
	for _, e := range s.Files() {
		out.Files = append(out.Files, fileInfo{ID: e.ID, Name: e.Name, Size: e.Size})
	}
	for _, f := range imageio.Formats() {
		if !f.CanEncode() {
			continue
		}
		ext := ""
		if len(f.Extensions) > 0 {
			ext = f.Extensions[0]
		}
		out.Formats = append(out.Formats, formatInfo{Name: f.Name, Ext: ext})
	}
	writeJSON(w, out)
}

// handleThumb decodes one file and returns a small PNG of it.
//
// It decodes untrusted input on demand, so it uses the same Limits the rest of the
// program uses — the header guard is not something a preview gets to skip.
func (s *Server) handleThumb(w http.ResponseWriter, r *http.Request) {
	target, err := s.Resolve(r.URL.Query().Get("f"))
	if err != nil {
		httpError(w, err, http.StatusForbidden)
		return
	}

	src, err := imageio.Open(target)
	if err != nil {
		httpError(w, err, http.StatusNotFound)
		return
	}
	defer src.Close()

	img, _, err := src.Decode(imageio.Limits{})
	if err != nil {
		httpError(w, err, http.StatusUnsupportedMediaType)
		return
	}
	small, err := convert.Resize(img, convert.Options{Width: thumbWidth})
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}

	png, ok := imageio.Lookup("png")
	if !ok {
		httpError(w, fmt.Errorf("png is not in the registry"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	if err := png.Encode(w, small, imageio.Options{}); err != nil {
		// The status is already written; there is nothing honest left to send.
		return
	}
}

// handleConvert converts the named files and reports each one.
//
// A file that fails is a RESULT, not a server error: the whole point of reporting
// per file is that one bad file does not cost the others. Only a request that is
// malformed as a whole gets a non-200.
func (s *Server) handleConvert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, fmt.Errorf("use POST"), http.StatusMethodNotAllowed)
		return
	}
	var req convertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	target, ok := imageio.Lookup(req.Format)
	if !ok {
		httpError(w, fmt.Errorf("%q is not a format this build knows", req.Format), http.StatusBadRequest)
		return
	}
	if !target.CanEncode() {
		httpError(w, fmt.Errorf("%s: decode only, cannot be a target format", target.Name), http.StatusBadRequest)
		return
	}

	results := make([]convertResult, 0, len(req.Files))
	for _, name := range req.Files {
		results = append(results, s.convertOne(name, target, req))
	}
	writeJSON(w, map[string]any{"results": results})
}

// convertOne is deliberately thin: it resolves the path, and everything after
// that is pipeline's. A front end with conversion logic of its own is how the
// CLI, the TUI and the directory run drifted apart once already.
func (s *Server) convertOne(name string, target imageio.Format, req convertRequest) convertResult {
	res := convertResult{}

	// The name is joined to the folder being browsed, and Resolve contains the
	// result. A name carrying a separator is not a file the page showed, so it is
	// refused before anything opens it.
	if strings.ContainsAny(name, `/\`) {
		res.Error = fmt.Sprintf("%s: a file name may not contain a path", name)
		return res
	}
	input, err := s.Resolve(name)
	if err != nil {
		// The id is all we have; naming it back is the only honest answer.
		res.Input = name
		res.Error = err.Error()
		return res
	}
	res.Input = filepath.Base(input)
	// The output goes beside its own input. It is not in the allowlist and must
	// not be: the allowlist says what may be READ, and imageio.Write is what
	// decides whether a destination may be written.
	output := pipeline.OutputPath(input, "", target)

	out, err := pipeline.Convert(pipeline.Request{
		Input:     input,
		Output:    output,
		Target:    target,
		Codec:     imageio.Options{Quality: req.Quality},
		Transform: convert.Options{Width: req.Width, Height: req.Height},
		Force:     req.Force,
	})
	res.Warning = out.Warning
	if err != nil {
		res.Error = browserMessage(err, input, output)
		return res
	}
	res.Output = filepath.Base(output)
	return res
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return
	}
}

// httpError sends the reason to the operator. This is a tool running on the
// operator's own machine at their request, so the real message is what they need;
// there is no third party here to leak an internal path to.
func httpError(w http.ResponseWriter, err error, code int) {
	http.Error(w, err.Error(), code)
}

// handlePage serves the interface as ONE self-contained document.
//
// The CSS and the JS are spliced in rather than linked, and that is a correctness
// fix, not a style choice: every request needs the run token, the page receives
// it in the URL, and a browser fetching <link href="app.css"> sends no token at
// all. Linked assets came back 403 and the operator got unstyled HTML while every
// handler test still passed, because the tests set the header by hand.
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	page, err := assets.ReadFile("assets/index.html")
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	css, err := assets.ReadFile("assets/app.css")
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	js, err := assets.ReadFile("assets/app.js")
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}

	body := strings.Replace(string(page), "/*__STYLE__*/", string(css), 1)
	body = strings.Replace(body, "/*__SCRIPT__*/", string(js), 1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The page holds the run token; a cached copy of it outlives the run.
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, body)
}

// pickRequest carries nothing. The dialog has one mode — choose files — because
// choosing a folder was a second way to do the same thing and the operator has to
// name files anyway. Every argument the dialog is launched with is a fixed string
// in this package, so a request controls no part of the command line.
type pickRequest struct{}

type pickResponse struct {
	Added   int  `json:"added"`
	Changed bool `json:"changed"`
}

// handlePick opens the operating system's own file dialog and moves browsing to
// whatever the operator chose.
//
// The dialog is opened by the SERVER rather than by the browser on purpose: a
// browser's file input hands the page bytes and hides the path, which would make
// this tool convert a copy and lose the place the result belongs. imgconv already
// runs on the operator's machine, so it can ask the desktop directly and get a
// real path back.
//
// A pick is the only thing that moves the ceiling, and it cannot happen without a
// person: holding the token is enough to make a dialog appear and nothing more.
func (s *Server) handlePick(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, fmt.Errorf("use POST"), http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	pick := s.picker
	s.mu.RUnlock()
	if pick == nil {
		httpError(w, ErrNoPicker, http.StatusNotImplemented)
		return
	}

	picked, err := pick()
	if err != nil {
		httpError(w, err, http.StatusNotImplemented)
		return
	}
	// Cancel. Nothing chosen, so nothing changes.
	if len(picked) == 0 {
		writeJSON(w, pickResponse{Changed: false})
		return
	}

	added := s.Add(picked)
	writeJSON(w, pickResponse{Added: len(added), Changed: true})
}

// handleRemove takes one file off the list. Nothing on disk is touched: the
// operator is un-choosing it, not deleting it.
func (s *Server) handleRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, fmt.Errorf("use POST"), http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	s.Remove(req.ID)
	w.WriteHeader(http.StatusOK)
}

// browserMessage turns an error into something this front end can show.
//
// Two things have to happen and both are about the operator rather than the code.
// An absolute path in the text puts their directory tree on screen, which is the
// thing they asked to be rid of, so every path is reduced to its file name. And a
// refusal to overwrite has to name the control THIS interface has: the checkbox,
// never the command-line flag that means the same thing somewhere else.
func browserMessage(err error, input, output string) string {
	msg := err.Error()
	for _, p := range []string{input, output, filepath.Dir(input), filepath.Dir(output)} {
		if p != "" && p != "." && p != string(filepath.Separator) {
			msg = strings.ReplaceAll(msg, p+string(filepath.Separator), "")
			msg = strings.ReplaceAll(msg, p, filepath.Base(p))
		}
	}
	if errors.Is(err, imageio.ErrExists) {
		msg += " (marque Substituir arquivo existente para trocar)"
	}
	return msg
}

// handleQuit shuts the server down at the operator's request.
//
// It exists because closing the window only stops the process when there IS a
// window: started from a file manager there is no controlling terminal, nothing
// sends SIGHUP, and the process is adopted by init and keeps listening. Stopping
// must not depend on how it was started.
//
// The reply is written and flushed BEFORE anything closes, so the page learns it
// worked rather than seeing a dropped connection and guessing.
func (s *Server) handleQuit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, fmt.Errorf("use POST"), http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"stopped":true}`)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	s.Stop()
}
