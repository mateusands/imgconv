// The contract for "Selecionar arquivos": the button that opens the operating
// system's own file dialog.
//
// WHY THE SERVER OPENS IT AND NOT THE BROWSER: a browser's <input type="file">
// does open the native dialog, but it hands the page the file's BYTES and hides
// its path on purpose. That is the upload model — the tool would convert a copy
// and could not put the result beside the original, which is the whole shape this
// program was built around. imgconv runs on the operator's own machine, so it can
// run the dialog itself and get a real path back.
//
// WHAT IS A RULE: the dialog is external and optional. A machine without kdialog
// or zenity must get a clear message, never a hang and never a crash. And a pick
// is only ever honoured when a human actually chose something — an empty return
// (they pressed cancel) changes nothing.
package go_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/mateusands/imgconv/internal/web"
)

// Superseded: what a pick does is now covered by selection_test.go, which asserts
// the chosen files become THE list rather than moving a browse root. Only the
// dialog's own behaviour is tested here.

// Cancel is the common case and must be inert.
func TestPick_ShouldChangeNothingWhenTheOperatorCancels(t *testing.T) {
	dir := t.TempDir()
	writePNGAt(t, filepath.Join(dir, "ficou.png"))
	s := newServer(t, dir)
	s.SetPicker(func(string) ([]string, error) { return nil, nil })

	rec := post(t, s, "/api/pick", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancelling is not an error: status = %d, %s", rec.Code, rec.Body.String())
	}
	if n := len(s.Files()); n != 0 {
		t.Errorf("%d file(s) entered the selection on a cancel; nothing was chosen", n)
	}
}

// A machine with no dialog installed still has a working tool; it just cannot use
// this button. The message has to say which, rather than failing silently.
func TestPick_ShouldExplainWhenNoNativeDialogIsInstalled(t *testing.T) {
	s := newServer(t, t.TempDir())
	s.SetPicker(func(string) ([]string, error) { return nil, ErrNoPicker })

	rec := post(t, s, "/api/pick", `{}`)
	if rec.Code == http.StatusOK {
		t.Fatal("a missing dialog must be reported, not swallowed")
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "kdialog") &&
		!strings.Contains(strings.ToLower(rec.Body.String()), "zenity") {
		t.Errorf("the message must name what to install, got %q", rec.Body.String())
	}
}
