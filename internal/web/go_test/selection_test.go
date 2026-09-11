// The contract after the operator saw the page listing their whole folder.
//
// WHY THIS REPLACED THE FOLDER LISTING: the page showed every image in the
// directory it was launched in, and the operator's reaction was the correct one —
// they had chosen nothing, so nothing should have been there. A converter is not
// a file manager.
//
// The consequence is bigger than the screen. The server no longer lists a
// directory at all: the set of files it may touch is exactly the set a human
// chose in the operating system's own dialog. Containment stops being a path
// comparison and becomes an ALLOWLIST, which cannot be reasoned around — there is
// no traversal to attempt when the only reachable paths are ones already chosen.
//
// WHAT IS A RULE: nothing is listed until it is picked; picking ADDS rather than
// replaces, because "add more files" is the normal way to build a batch; and a
// name that was never picked is refused whatever it looks like.
package go_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	. "github.com/mateusands/imgconv/internal/web"
)

type listed struct {
	Files []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Size int64  `json:"size"`
	} `json:"files"`
}

func currentList(t *testing.T, s *Server) listed {
	t.Helper()
	var out listed
	if err := json.Unmarshal(get(t, s, "/api/files").Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding the listing: %v", err)
	}
	return out
}

func TestFiles_ShouldStartEmptyEvenWhenTheFolderIsFullOfImages(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.png", "b.png", "c.png"} {
		writePNGAt(t, filepath.Join(dir, n))
	}
	s := newServer(t, dir)

	if got := currentList(t, s); len(got.Files) != 0 {
		t.Errorf("%d file(s) listed before anything was chosen; the operator picked none of them", len(got.Files))
	}
}

func TestPick_ShouldListOnlyWhatWasChosen(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"escolhida.png", "ignorada.png"} {
		writePNGAt(t, filepath.Join(dir, n))
	}
	s := newServer(t, dir)
	s.SetPicker(func(string) ([]string, error) {
		return []string{filepath.Join(dir, "escolhida.png")}, nil
	})

	if rec := post(t, s, "/api/pick", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	got := currentList(t, s)
	if len(got.Files) != 1 || got.Files[0].Name != "escolhida.png" {
		t.Errorf("listed %+v, want only escolhida.png — its neighbour was never chosen", got.Files)
	}
}

// "Adicionar mais ficheiros" is the normal way a batch gets built, and files
// chosen in a second pass may live in a different folder.
func TestPick_ShouldAddToTheSelectionRatherThanReplaceIt(t *testing.T) {
	one, two := t.TempDir(), t.TempDir()
	writePNGAt(t, filepath.Join(one, "primeira.png"))
	writePNGAt(t, filepath.Join(two, "segunda.png"))

	s := newServer(t, one)
	s.SetPicker(func(string) ([]string, error) { return []string{filepath.Join(one, "primeira.png")}, nil })
	post(t, s, "/api/pick", `{}`)

	s.SetPicker(func(string) ([]string, error) { return []string{filepath.Join(two, "segunda.png")}, nil })
	post(t, s, "/api/pick", `{}`)

	got := currentList(t, s)
	if len(got.Files) != 2 {
		t.Fatalf("listed %d file(s), want both picks kept: %+v", len(got.Files), got.Files)
	}
	names := map[string]bool{got.Files[0].Name: true, got.Files[1].Name: true}
	if !names["primeira.png"] || !names["segunda.png"] {
		t.Errorf("got %v — files from two different folders must both survive", names)
	}
}

func TestSelection_ShouldForgetAFileTheOperatorRemoves(t *testing.T) {
	dir := t.TempDir()
	writePNGAt(t, filepath.Join(dir, "some.png"))
	s := newServer(t, dir)
	s.SetPicker(func(string) ([]string, error) { return []string{filepath.Join(dir, "some.png")}, nil })
	post(t, s, "/api/pick", `{}`)

	id := currentList(t, s).Files[0].ID
	if rec := post(t, s, "/api/remove", `{"id":"`+id+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("removing: status = %d, %s", rec.Code, rec.Body.String())
	}
	if got := currentList(t, s); len(got.Files) != 0 {
		t.Errorf("still listing %+v after it was removed", got.Files)
	}
}

// The allowlist, stated as a test: a path nobody chose is unreachable, and it does
// not matter whether it exists, whether it is an image, or how it is spelled.
func TestConvert_ShouldRefuseAFileThatWasNeverPicked(t *testing.T) {
	dir := t.TempDir()
	writePNGAt(t, filepath.Join(dir, "nao-escolhida.png"))
	s := newServer(t, dir)

	for _, attempt := range []string{"nao-escolhida.png", "/etc/passwd", "../../etc/passwd", "0", ""} {
		rec := post(t, s, "/api/convert", `{"files":["`+attempt+`"],"format":"png"}`)
		if rec.Code != http.StatusOK {
			continue // refused outright is also correct
		}
		var got struct {
			Results []struct {
				Error string `json:"error"`
			} `json:"results"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Results) == 1 && got.Results[0].Error == "" {
			t.Errorf("%q was converted; only a file the operator picked may be", attempt)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "nao-escolhida.png")); err != nil {
		t.Errorf("the untouched file should still be there: %v", err)
	}
}

func TestThumb_ShouldRefuseAnIdThatWasNeverPicked(t *testing.T) {
	s := newServer(t, t.TempDir())
	if rec := get(t, s, "/api/thumb?f=whatever"); rec.Code == http.StatusOK {
		t.Error("a thumbnail was served for something nobody chose")
	}
}

// The operator asked to see what a picked file actually is.
//
// A name and a byte count do not answer the question that matters before a
// conversion — how big is this picture, and what is it really. The second half of
// that is not rhetorical here: this program decides format by BYTES, so a file
// named .jpeg can be a PNG, and the list is the only place that difference is
// visible before something is written.
//
// The dimensions are read from the header, never by decoding: doing it the other
// way would allocate every pixel of every file in the list, on the one screen
// where a decode bomb is most likely to arrive by accident.
func TestFiles_ShouldDescribeEachChosenFile(t *testing.T) {
	dir := t.TempDir()
	// PNG bytes under a name that claims JPEG.
	liar := filepath.Join(dir, "mentiroso.jpeg")
	writePNGAt(t, liar)

	s := newServer(t, dir)
	s.SetPicker(func(string) ([]string, error) { return []string{liar}, nil })
	post(t, s, "/api/pick", `{}`)

	var got struct {
		Files []struct {
			Name   string `json:"name"`
			Format string `json:"format"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
			Size   int64  `json:"size"`
		} `json:"files"`
	}
	if err := json.Unmarshal(get(t, s, "/api/files").Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 1 {
		t.Fatalf("expected the one picked file, got %d", len(got.Files))
	}
	f := got.Files[0]
	if f.Format != "png" {
		t.Errorf("format = %q, want png — the name says jpeg and the bytes decide", f.Format)
	}
	// writePNGAt makes a 16x12 image; the helper is the contract for those numbers.
	if f.Width != 16 || f.Height != 12 {
		t.Errorf("dimensions = %dx%d, want 16x12", f.Width, f.Height)
	}
	if f.Size <= 0 {
		t.Error("size is still part of the description")
	}
}

// A file that cannot be described is still listed. Dropping it would hide a
// choice the operator made, and the conversion will report the real reason.
func TestFiles_ShouldStillListAFileItCannotDescribe(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "quebrado.jpg")
	if err := os.WriteFile(broken, []byte("nao e imagem"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newServer(t, dir)
	s.SetPicker(func(string) ([]string, error) { return []string{broken}, nil })
	post(t, s, "/api/pick", `{}`)

	var got struct {
		Files []struct {
			Name   string `json:"name"`
			Format string `json:"format"`
			Width  int    `json:"width"`
		} `json:"files"`
	}
	if err := json.Unmarshal(get(t, s, "/api/files").Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 1 || got.Files[0].Name != "quebrado.jpg" {
		t.Fatalf("the file the operator chose must still appear: %+v", got.Files)
	}
	if got.Files[0].Format != "" || got.Files[0].Width != 0 {
		t.Errorf("nothing is known about it, so nothing should be claimed: %+v", got.Files[0])
	}
}
