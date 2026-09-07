// The contract for what the page can ask for.
//
// WHY: these handlers are the only reason the guards in server_test.go exist, and
// they are where the front-end rules this project already carries have to hold
// again — the list is decided by BYTES and not by extension, a decode-only format
// is never offered as a target, and every conversion goes through the same
// pipeline the CLI and the TUI use. A third front end that converts its own way
// is the bug this project has already had once.
package go_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/mateusands/imgconv/internal/web"
)

func get(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Imgconv-Token", s.Token())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func post(t *testing.T, s *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("X-Imgconv-Token", s.Token())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

type listing struct {
	Root  string `json:"root"`
	Files []struct {
		Name string `json:"name"`
	} `json:"files"`
	Formats []struct {
		Name string `json:"name"`
	} `json:"formats"`
}

// choose puts files into the selection the way the operator would, and returns
// their ids. Everything the page can reach goes through here.
func choose(t *testing.T, s *Server, paths ...string) []string {
	t.Helper()
	var ids []string
	for _, e := range s.Add(paths) {
		ids = append(ids, e.ID)
	}
	if len(ids) < len(paths) {
		t.Fatalf("Add kept %d of %d files", len(ids), len(paths))
	}
	return ids
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writePNGAt(t, filepath.Join(root, "paisagem.png"))
	writePNGAt(t, filepath.Join(root, "disfarcado.txt")) // PNG bytes, .txt name
	if err := os.WriteFile(filepath.Join(root, "quebrado.jpg"), []byte("nao e imagem"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notas.txt"), []byte("texto"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// Rewritten when the folder listing was removed. It used to assert that a file
// whose bytes are not an image never appeared in the list. There is no list to
// keep it out of any more — the operator chose it in a dialog, so it is shown.
//
// The rule it protects survives, and it moved to a better place: a name that lies
// is not silently dropped, it FAILS LOUDLY and says why. Hiding a file somebody
// explicitly picked would be the silent failure this project forbids.
func TestConvert_ShouldFailLoudlyWhenAChosenFileIsNotAnImage(t *testing.T) {
	dir := fixtureRoot(t)
	s := newServer(t, dir)
	ids := choose(t, s, filepath.Join(dir, "quebrado.jpg"), filepath.Join(dir, "disfarcado.txt"))

	rec := post(t, s, "/api/convert", `{"files":["`+ids[0]+`","`+ids[1]+`"],"format":"png"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Results []struct {
			Input  string `json:"input"`
			Output string `json:"output"`
			Error  string `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 2 {
		t.Fatalf("every chosen file must be accounted for, got %d", len(got.Results))
	}
	for _, r := range got.Results {
		switch r.Input {
		case "quebrado.jpg":
			if r.Error == "" {
				t.Error("a .jpg holding text must be reported as a failure, not converted")
			}
		case "disfarcado.txt":
			// PNG bytes under a .txt name: the bytes decide, so it converts.
			if r.Error != "" {
				t.Errorf("its bytes are a PNG, so it must convert: %s", r.Error)
			}
		}
	}
}

func TestFiles_ShouldOfferOnlyFormatsThatCanBeEncoded(t *testing.T) {
	s := newServer(t, fixtureRoot(t))

	var got listing
	if err := json.Unmarshal(get(t, s, "/api/files").Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, f := range got.Formats {
		if f.Name == "webp" {
			t.Error("webp has no encoder; offering it promises a conversion that cannot happen")
		}
	}
	if len(got.Formats) < 4 {
		t.Errorf("expected at least jpeg, png, gif and tiff; got %d", len(got.Formats))
	}
}

func TestThumb_ShouldRefuseAFileOutsideTheRoot(t *testing.T) {
	s := newServer(t, fixtureRoot(t))

	if rec := get(t, s, "/api/thumb?f=../../etc/passwd"); rec.Code == http.StatusOK {
		t.Error("a thumbnail request escaped the root")
	}
}

func TestConvert_ShouldReportTheOverwriteRefusalInsteadOfReplacingTheFile(t *testing.T) {
	root := t.TempDir()
	writePNGAt(t, filepath.Join(root, "foto.png"))
	s := newServer(t, root)
	ids := choose(t, s, filepath.Join(root, "foto.png"))

	// png -> png targets the input itself. The guard must refuse, and the refusal
	// has to reach the response rather than being swallowed into a 500.
	rec := post(t, s, "/api/convert", `{"files":["`+ids[0]+`"],"format":"png"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: a per-file refusal is a result, not a server error: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "already exists") && !strings.Contains(rec.Body.String(), "refusing") {
		t.Errorf("the refusal did not reach the operator: %s", rec.Body.String())
	}
}

func TestConvert_ShouldReportThatFramesWereDroppedWhenFlatteningAnAnimatedGif(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "animado.gif"), animatedGIFAt(t, 4), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newServer(t, root)
	ids := choose(t, s, filepath.Join(root, "animado.gif"))

	rec := post(t, s, "/api/convert", `{"files":["`+ids[0]+`"],"format":"png"}`)
	if !strings.Contains(rec.Body.String(), "frame") {
		t.Errorf("flattening silently is the failure this warning exists for: %s", rec.Body.String())
	}
}

// Superseded by TestConvert_ShouldRefuseAFileThatWasNeverPicked in
// selection_test.go. An escape attempt is no longer a path to contain, it is an
// id that is not in the allowlist, and that is a stronger statement than the one
// this test used to make.

func TestConvert_ShouldConvertAndLeaveTheInputByteIdentical(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "foto.png")
	writePNGAt(t, input)
	before, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	s := newServer(t, root)
	ids := choose(t, s, input)

	rec := post(t, s, "/api/convert", `{"files":["`+ids[0]+`"],"format":"jpeg","quality":80}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "foto.jpg")); err != nil {
		t.Errorf("the output was not written: %v", err)
	}
	after, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("the input changed: had %d bytes, now has %d", len(before), len(after))
	}
}

// The bug this catches, which every other test on this page missed: the guard
// requires a token on every request, the page is opened with the token in the
// URL, and a browser then asks for <link href="app.css"> and <script src="app.js">
// WITHOUT it. Both came back 403 and the operator got unstyled HTML.
//
// Every handler test here passed while that was true, because they all set the
// header by hand — which a browser fetching a sub-resource does not do. The page
// is therefore served self-contained, and this asserts it stays that way.
func TestPage_ShouldServeAStyledPageInOneRequest(t *testing.T) {
	s := newServer(t, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/?t="+s.Token(), nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<style") {
		t.Error("no stylesheet in the page: a browser would ask for it in a second request that carries no token")
	}
	if !strings.Contains(body, "<script") {
		t.Error("no script in the page, for the same reason")
	}
	// A sub-resource reference is the shape of the bug. Nothing may need a second
	// request to make the page work.
	for _, ref := range []string{`href="app.css"`, `src="app.js"`} {
		if strings.Contains(body, ref) {
			t.Errorf("page still references %s as a separate request; it will be refused for having no token", ref)
		}
	}
}

// Browsing was REMOVED after the operator saw it on screen. With the system's own
// file dialog doing the choosing, a folder tree inside the page had no job left,
// and it put the absolute path of someone's machine on display for no gain. The
// three tests that covered navigating into a folder, refusing to climb above the
// ceiling by listing, and writing into a browsed subfolder went with the feature.
//
// What did NOT go is containment. Resolve still guards every name that reaches a
// file, and the tests for "..", an absolute path, a symlink and a sibling
// directory are untouched in server_test.go. They now guard a smaller surface,
// which is the point of removing a feature rather than hiding its buttons.

// The refusal has to speak the language of the front end the operator is using.
//
// imageio's message used to end "(pass --force to replace it)", which is sound
// advice on a command line and nonsense in a browser, where the same choice is a
// checkbox. A front end that repeats another front end's vocabulary teaches the
// operator that the message is not really about them.
func TestConvert_ShouldNotTellABrowserOperatorToPassACommandLineFlag(t *testing.T) {
	root := t.TempDir()
	writePNGAt(t, filepath.Join(root, "foto.png"))
	s := newServer(t, root)
	ids := choose(t, s, filepath.Join(root, "foto.png"))

	// png -> png aims at the input itself, so the guard refuses.
	body := post(t, s, "/api/convert", `{"files":["`+ids[0]+`"],"format":"png"}`).Body.String()

	if strings.Contains(body, "--force") {
		t.Errorf("the browser was told to pass a command-line flag: %s", body)
	}
	if !strings.Contains(strings.ToLower(body), "substituir") {
		t.Errorf("the refusal must point at the control this operator actually has: %s", body)
	}
}

// The page must say where the result lands.
//
// It became necessary the moment the folder stopped being shown: the operator
// picks a file from anywhere on disk and the interface no longer displays a path,
// so without this sentence there is nothing on screen answering "where does the
// converted file go" at the moment they press Convert. It also states the
// guarantee the whole program is built around — the original is not touched —
// which is worth saying out loud rather than leaving as something to discover.
func TestPage_ShouldSayWhereTheConvertedFileGoes(t *testing.T) {
	s := newServer(t, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/?t="+s.Token(), nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	body := strings.ToLower(rec.Body.String())

	if !strings.Contains(body, "mesma pasta") {
		t.Error("the page never says the result lands beside the original")
	}
	if !strings.Contains(body, "nunca e alterado") && !strings.Contains(body, "nunca é alterado") {
		t.Error("the page never states that the original is left alone, which is the guarantee this program exists for")
	}
}

// The page must be able to stop the server, and this test exists because of how
// the gap was found: the operator asked whether closing the window drops the
// server, and the honest answer turned out to be "only if you started it from a
// terminal". Double-clicked from a file manager there is no controlling terminal,
// nothing sends SIGHUP, and the process is adopted by init and keeps listening —
// a local server running invisibly, which is the thing they wanted to avoid.
//
// So stopping it cannot depend on how it was started. The interface that started
// the work is the interface that ends it.
func TestQuit_ShouldStopTheServer(t *testing.T) {
	s := newServer(t, t.TempDir())
	if err := s.Listen(); err != nil {
		t.Fatal(err)
	}

	served := make(chan error, 1)
	go func() { served <- s.Serve() }()

	if rec := post(t, s, "/api/quit", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return; the server is still listening after being asked to stop")
	}
}

// Stopping is as consequential as writing, so it lives behind the same guard.
func TestQuit_ShouldRefuseARequestWithoutTheToken(t *testing.T) {
	s := newServer(t, t.TempDir())

	req := httptest.NewRequest(http.MethodPost, "/api/quit", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 — anything local could shut the server down", rec.Code)
	}
}

// The page shipped once with its "server stopped" overlay covering everything on
// first load, and the cause is a trap worth a permanent test: the hidden
// attribute is enforced by the BROWSER's stylesheet, and any author rule that
// sets display outranks it. Three rules on this page do — .rows is flex, label is
// block, .stopped is grid — so el.hidden = true silently meant nothing.
//
// A stylesheet cannot be exercised here, so this asserts the fix is present. It
// is a weaker test than running a browser and a much better one than the nothing
// that let this reach the operator.
func TestPage_ShouldForceHiddenToActuallyHide(t *testing.T) {
	s := newServer(t, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/?t="+s.Token(), nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()

	if !strings.Contains(body, "[hidden]") {
		t.Fatal("no [hidden] rule in the stylesheet: every element marked hidden will still render")
	}
	if !strings.Contains(body, "display: none !important") {
		t.Error("the [hidden] rule does not outrank the author display rules it exists to beat")
	}
}
