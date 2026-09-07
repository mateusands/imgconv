package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateusands/imgconv/internal/imageio"
	"github.com/mateusands/imgconv/internal/pipeline"
)

// stage is where the operator is in the flow, and the only mode this Model has:
// every key press is read against this one value.
type stage int

const (
	stagePickFile stage = iota
	stagePickFormat
	stageConverting
	stageDone
)

// Model is the whole state of the interface. It is exported, like NewModel, so the
// tests in go_test/ can drive the loop without a terminal: Update is a pure
// function of (Model, Msg), which is what makes that possible at all.
//
// Model is the whole state of the interface. Bubble Tea is the Elm architecture:
// Update returns a NEW Model rather than mutating this one — the value receivers
// throughout are what make that guarantee cheap and hard to get wrong.
type Model struct {
	dir     string
	files   []string
	formats []imageio.Format

	fileCursor   int
	formatCursor int
	stage        stage

	// chosen is the picked file's path, copied out of the list at the moment it is
	// picked. Holding the index instead would leave the later screens reading a
	// slice that a listing message is allowed to replace underneath them.
	chosen string

	// listed separates "the directory came back empty" from "the listing command
	// has not answered yet". Without it the first frame lies about an empty folder.
	listed  bool
	listErr error

	result convertedMsg
}

func NewModel(dir string) Model {
	return Model{dir: dir, formats: TargetFormats()}
}

// listedMsg carries the result of reading the directory, and convertedMsg the
// result of one conversion. Messages are the only way work done off the loop gets
// back into it.
type listedMsg struct {
	files []string
	err   error
}

type convertedMsg struct {
	input   string
	output  string
	warning string
	err     error
}

func (m Model) Init() tea.Cmd { return listCmd(m.dir) }

// listCmd reads the directory off the update loop. Sniffing opens and parses the
// header of every file in it: fast for one file, unbounded over a directory, and
// anything unbounded inside Update freezes the whole interface.
func listCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		files, err := ListImages(dir)
		return listedMsg{files: files, err: err}
	}
}

// convertCmd runs one conversion and reports it as a message.
//
// It takes the same three steps the CLI takes — imageio decodes, convert
// transforms, imageio writes — because a front end with a conversion path of its
// own is a bug and not a feature of the TUI. force is deliberately false: the TUI
// has no flag, so an existing destination is a refusal the operator reads rather
// than something a keystroke can override.
func convertCmd(input string, target imageio.Format) tea.Cmd {
	return func() tea.Msg {
		out, err := pipeline.Convert(pipeline.Request{
			Input:  input,
			Output: pipeline.OutputPath(input, "", target),
			Target: target,
		})
		// Empty codec and transform options: the TUI offers neither quality nor
		// resize in v1. It goes through the shared pipeline anyway so that the two
		// front ends cannot drift apart, which is exactly what they had done.
		return convertedMsg{
			input:   input,
			output:  pipeline.OutputPath(input, "", target),
			warning: out.Warning,
			err:     err,
		}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case listedMsg:
		m.files, m.listErr, m.listed = msg.files, msg.err, true
		m.fileCursor = MoveCursor(m.fileCursor, 0, len(m.files))
		return m, nil

	case convertedMsg:
		m.result, m.stage = msg, stageDone
		return m, nil

	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return m, nil
}

func (m Model) onKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	}

	switch m.stage {
	case stagePickFile:
		switch key.String() {
		case "up", "k":
			m.fileCursor = MoveCursor(m.fileCursor, -1, len(m.files))
		case "down", "j":
			m.fileCursor = MoveCursor(m.fileCursor, +1, len(m.files))
		case "enter":
			if len(m.files) > 0 {
				m.chosen = filepath.Join(m.dir, m.files[m.fileCursor])
				m.stage = stagePickFormat
			}
		}

	case stagePickFormat:
		switch key.String() {
		case "up", "k":
			m.formatCursor = MoveCursor(m.formatCursor, -1, len(m.formats))
		case "down", "j":
			m.formatCursor = MoveCursor(m.formatCursor, +1, len(m.formats))
		case "backspace", "left":
			m.stage = stagePickFile
		case "enter":
			if len(m.formats) == 0 {
				return m, nil
			}
			m.stage = stageConverting
			return m, convertCmd(m.chosen, m.formats[m.formatCursor])
		}

	case stageDone:
		if key.String() == "enter" {
			m.result, m.chosen, m.stage = convertedMsg{}, "", stagePickFile
			return m, listCmd(m.dir)
		}
	}

	// stageConverting reaches here and ignores the key: the only work in flight is
	// the conversion, and there is nothing to steer while it runs.
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("imgconv"))
	b.WriteString("\n\n")

	switch m.stage {
	case stagePickFile:
		b.WriteString(m.viewFiles())
	case stagePickFormat:
		b.WriteString(m.viewFormats())
	case stageConverting:
		b.WriteString(fmt.Sprintf("converting %s to %s...\n",
			filepath.Base(m.chosen), m.formats[m.formatCursor].Name))
	case stageDone:
		b.WriteString(m.viewResult())
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render(m.help()))
	b.WriteString("\n")
	return b.String()
}

func (m Model) viewFiles() string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("file to convert"))
	b.WriteString("  ")
	b.WriteString(helpStyle.Render(m.dir))
	b.WriteString("\n\n")

	switch {
	case m.listErr != nil:
		b.WriteString(errorStyle.Render(fmt.Sprintf("cannot read the directory: %v", m.listErr)))
		b.WriteString("\n")
	case !m.listed:
		b.WriteString("reading the directory...\n")
	case len(m.files) == 0:
		b.WriteString("no image files here.\n")
		b.WriteString(helpStyle.Render("a file is listed when its bytes decode as an image, whatever it is named"))
		b.WriteString("\n")
	default:
		b.WriteString(renderList(m.files, m.fileCursor))
	}
	return b.String()
}

func (m Model) viewFormats() string {
	names := make([]string, len(m.formats))
	for i, f := range m.formats {
		names[i] = f.Name
	}

	var b strings.Builder
	b.WriteString(headingStyle.Render("convert to"))
	b.WriteString("  ")
	b.WriteString(helpStyle.Render(filepath.Base(m.chosen)))
	b.WriteString("\n\n")
	b.WriteString(renderList(names, m.formatCursor))
	if len(m.formats) > 0 {
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("writes " + pipeline.OutputPath(m.chosen, "", m.formats[m.formatCursor])))
		b.WriteString("\n")
	}
	return b.String()
}

// viewResult always says which file the outcome is about. An error the operator
// cannot attribute to a file is barely better than silence, and silent failure is
// the worst defect this program can have.
func (m Model) viewResult() string {
	var b strings.Builder
	if m.result.err != nil {
		b.WriteString(errorStyle.Render("could not convert " + filepath.Base(m.result.input)))
		b.WriteString("\n")
		b.WriteString(m.result.err.Error())
		b.WriteString("\n")
		return b.String()
	}

	b.WriteString(successStyle.Render("converted"))
	b.WriteString("\n")
	b.WriteString(pathStyle.Render(m.result.output))
	b.WriteString("\n")
	if m.result.warning != "" {
		b.WriteString(warningStyle.Render(m.result.warning))
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) help() string {
	switch m.stage {
	case stagePickFile:
		return "up/down move  enter choose  q quit"
	case stagePickFormat:
		return "up/down move  enter convert  backspace back  q quit"
	case stageConverting:
		return "working..."
	default:
		return "enter convert another  q quit"
	}
}

func renderList(items []string, cursor int) string {
	var b strings.Builder
	for i, item := range items {
		if i == cursor {
			b.WriteString(selectedStyle.Render(cursorMarker + item))
		} else {
			b.WriteString(itemStyle.Render(blankMarker + item))
		}
		b.WriteString("\n")
	}
	return b.String()
}
