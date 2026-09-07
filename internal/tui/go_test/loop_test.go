// The TUI's loop, driven WITHOUT a terminal.
//
// WHY THIS EXISTS: the approved plan says the TUI ships with no automated test and
// that acceptance criterion A8 is human-verified only. That is still true of the
// RENDERING — colours, terminal restore, resize — and nothing here claims
// otherwise. But Bubble Tea is the Elm architecture, so Update is a pure function
// of (Model, Msg) and View is a pure function of Model. Everything the interface
// DECIDES can therefore be checked here, and leaving it unchecked because the
// pixels cannot be was confusing two different questions.
//
// WHAT IS A RULE AND WHAT IS AN ACCIDENT: that the picker lists by bytes and not
// by extension is a rule. That a decode-only format is never offered as a target
// is a rule. That the first frame renders BEFORE the directory has been read is
// a rule too, and the sharpest one here — a listing that happened inside Update
// would pass every other test on this page while freezing the interface on any
// directory large enough to matter.
package go_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	. "github.com/mateusands/imgconv/internal/tui"
)

// drive runs a tea.Cmd and feeds its message back into the model, which is what
// the Bubble Tea runtime does between frames.
func drive(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

func key(m Model, k string) Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	return next.(Model)
}

func fixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "photo.png"))
	writePNG(t, filepath.Join(dir, "fake.txt")) // image bytes, non-image name
	if err := os.WriteFile(filepath.Join(dir, "broken.jpg"), []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Checklist 12, and the one worth the most: the header must be on screen before
// the directory has been read. If listing ever moves into Update this fails.
func TestLoop_ShouldRenderTheFirstFrameBeforeTheDirectoryHasBeenRead(t *testing.T) {
	m := NewModel(fixtureDir(t))

	first := m.View()
	if first == "" {
		t.Fatal("the first frame is empty: there is nothing on screen while the listing runs")
	}
	if strings.Contains(first, "photo.png") {
		t.Error("the first frame already lists files, so the directory was read inside the loop, not in a command")
	}
	if m.Init() == nil {
		t.Error("Init returns no command, so the listing has nowhere to run except the update loop")
	}
}

// Checklist 2: the list is decided by bytes, never by the name.
func TestLoop_ShouldListOnlyTheFilesWhoseBytesAreAnImage(t *testing.T) {
	dir := fixtureDir(t)
	m := NewModel(dir)
	m = drive(m, m.Init())

	view := m.View()
	for _, want := range []string{"photo.png", "fake.txt"} {
		if !strings.Contains(view, want) {
			t.Errorf("%s decodes as an image and must be offered; view was:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"broken.jpg", "notes.txt"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("%s is not an image and must not be listed; the extension was trusted", unwanted)
		}
	}
}

// Checklist 4: a decode-only format may never be offered as a target.
func TestLoop_ShouldNeverOfferADecodeOnlyFormatAsATarget(t *testing.T) {
	dir := fixtureDir(t)
	m := NewModel(dir)
	m = drive(m, m.Init())

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	view := m.View()
	if strings.Contains(view, "webp") {
		t.Error("webp has no encoder; offering it as a target promises a conversion that cannot happen")
	}
	for _, want := range []string{"jpeg", "png", "gif", "tiff"} {
		if !strings.Contains(view, want) {
			t.Errorf("%s can encode and must be offered; view was:\n%s", want, view)
		}
	}
}

// Checklist 3: the cursor clamps at both ends rather than wrapping.
func TestLoop_ShouldClampTheCursorRatherThanWrapIt(t *testing.T) {
	dir := fixtureDir(t)
	m := NewModel(dir)
	m = drive(m, m.Init())

	up := key(m, "k").View()
	if up != m.View() {
		t.Error("pressing up on the first row moved the cursor; it must clamp")
	}

	down := m
	for i := 0; i < 10; i++ {
		down = key(down, "j")
	}
	if key(down, "j").View() != down.View() {
		t.Error("pressing down past the last row moved the cursor; it must clamp")
	}
}

// Checklist 6 and 7 together: a conversion writes the output and leaves the input
// byte-identical. This is the acceptance criterion the whole project exists for.
func TestLoop_ShouldConvertAndLeaveTheInputByteIdentical(t *testing.T) {
	dir := fixtureDir(t)
	input := filepath.Join(dir, "photo.png")
	before, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(dir)
	m = drive(m, m.Init())

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // pick the file
	m = next.(Model)
	// Move to a format that is not png so the output is a different file.
	m = key(m, "j")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // pick the format
	m = drive(next.(Model), cmd)

	view := m.View()
	if strings.Contains(strings.ToLower(view), "could not convert") {
		t.Fatalf("the conversion failed:\n%s", view)
	}

	after, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("the input changed: had %d bytes, now has %d", len(before), len(after))
	}
}

// Checklist 8: converting a file onto an existing output is refused, visibly, and
// the refusal reaches the screen rather than being swallowed.
func TestLoop_ShouldShowTheRefusalWhenTheOutputAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "photo.png"))

	m := NewModel(dir)
	m = drive(m, m.Init())
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	// The first format is jpeg; picking png targets photo.png, which IS the input.
	for !strings.Contains(m.View(), "photo.png") {
		m = key(m, "j")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drive(next.(Model), cmd)

	view := strings.ToLower(m.View())
	if !strings.Contains(view, "could not convert") && !strings.Contains(view, "refus") && !strings.Contains(view, "already exists") {
		t.Errorf("a refused conversion must be visible on screen, got:\n%s", m.View())
	}
}

// Checklist 10: flattening an animated GIF says so.
func TestLoop_ShouldSayThatFramesWereDroppedWhenFlatteningAnAnimatedGif(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "moving.gif"), animatedGIFFixture(t, 3), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewModel(dir)
	m = drive(m, m.Init())
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	for !strings.Contains(m.View(), "png") {
		m = key(m, "j")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drive(next.(Model), cmd)

	if !strings.Contains(m.View(), "frame") {
		t.Errorf("dropping frames silently is the failure this warning exists for, got:\n%s", m.View())
	}
}

// Checklist 11: every quit key quits, from every stage.
func TestLoop_ShouldQuitOnEveryQuitKeyFromEveryStage(t *testing.T) {
	dir := fixtureDir(t)
	base := NewModel(dir)
	base = drive(base, base.Init())

	picked, _ := base.Update(tea.KeyMsg{Type: tea.KeyEnter})

	stages := map[string]tea.Model{"pick file": base, "pick format": picked}
	for name, m := range stages {
		for _, k := range []tea.KeyMsg{
			{Type: tea.KeyRunes, Runes: []rune("q")},
			{Type: tea.KeyCtrlC},
			{Type: tea.KeyEsc},
		} {
			if _, cmd := m.Update(k); cmd == nil {
				t.Errorf("%s: %v did not quit", name, k)
			}
		}
	}
}

// Checklist 13: an empty directory says so instead of rendering a blank list.
func TestLoop_ShouldSayTheDirectoryIsEmptyRatherThanRenderNothing(t *testing.T) {
	m := NewModel(t.TempDir())
	m = drive(m, m.Init())

	view := strings.ToLower(m.View())
	if !strings.Contains(view, "no image") && !strings.Contains(view, "empty") {
		t.Errorf("an empty directory must say so, got:\n%s", m.View())
	}
}
