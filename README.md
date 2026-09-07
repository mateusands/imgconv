# imgconv

A small command-line image converter written in Go. It converts between **JPEG, PNG, GIF, TIFF and BMP**
(and reads **WebP**), optionally resizing and setting JPEG quality — one file, or a whole directory
in parallel. It has two front ends over one core: flags for scripting, and an interactive terminal UI
when you run it with no arguments.

It is built around one promise: **it never damages the file you gave it.**

> This is a learning project — the author is using it to learn Go. It is written to be read, so the
> reasoning behind each decision is in the code and in [`AGENTS.md`](AGENTS.md).

## Install

**Download one file and run it.** imgconv is a single static executable with nothing to install
alongside it — no runtime, no libraries, no Go. Uninstalling is deleting the file.

Grab the one for your machine from [Releases](https://github.com/mateusands/imgconv/releases):

| Your machine | File |
|---|---|
| Windows | `imgconv-<version>-windows-amd64.exe` |
| macOS, Apple Silicon (M1 and later) | `imgconv-<version>-darwin-arm64` |
| macOS, Intel | `imgconv-<version>-darwin-amd64` |
| Linux | `imgconv-<version>-linux-amd64` |

`SHA256SUMS` is published beside them if you want to check what you downloaded.

### Opening the graphical interface

Running it with `--ui` starts a small local server, opens your browser, and prints the address.
**Closing the terminal window stops the server** — there is nothing left running in the background.

**Windows** — double-click the `.exe`. A console window opens, the browser follows. To have it start
in the interface every time, make a shortcut and add ` --ui` to the target.

**macOS** — `chmod +x imgconv-*-darwin-*` once, then double-click it; Finder runs it in Terminal.

**Linux** — from a terminal:

```bash
chmod +x imgconv-*-linux-amd64
./imgconv-*-linux-amd64 --ui
```

To double-click it instead, file managers need a launcher. Save this as
`~/.local/share/applications/imgconv.desktop`, with the path corrected:

```ini
[Desktop Entry]
Type=Application
Name=imgconv
Exec=/full/path/to/imgconv --ui
Terminal=true
Categories=Graphics;
```

> ⚠️ **These binaries are not code-signed.** macOS Gatekeeper and Windows SmartScreen will warn about
> an unidentified developer, because signing needs a paid certificate from Apple and Microsoft. On
> macOS, right-click → Open the first time; on Windows, "More info" → "Run anyway". If that trade is
> not one you want to make, build it yourself — the next section is three lines.

### Running it from a clone

There is a launcher for each platform, so the command does not have to be typed every time:

| Your machine | Double-click |
|---|---|
| Linux | `run.sh` |
| macOS | `run.command` |
| Windows | `run.bat` |

**Each one rebuilds before it starts.** That is deliberate rather than wasteful: the interface is
compiled into the binary with `go:embed`, so a change to the page does nothing until the binary is
rebuilt — and running a stale binary looks exactly like a change that did not work. A second of
compiling is cheaper than that confusion. Closing the window stops the server.

### Building it yourself

Requires Go (see the `go` line in [`go.mod`](go.mod)).

```bash
git clone https://github.com/mateusands/imgconv.git
cd imgconv
go build -o imgconv ./cmd/imgconv
```

To produce the binaries for every platform at once, from any one of them:

```bash
./scripts/build-release.sh v0.1.0     # writes dist/
```

That works with no cross-compiler installed because the build sets `CGO_ENABLED=0` and imgconv is
pure Go. It is also the reason the graphical interface is served to your browser rather than drawn
with a native toolkit: every native GUI library for Go needs cgo, and that would cost the single-file
build and the one-command cross-compile.

## Usage

```
imgconv --ui                             the graphical interface, in your browser
imgconv <input> -o <output>              convert one file; -o's extension picks the format
imgconv <input> --to png                 convert one file, beside the input
imgconv <dir> --to png --outdir <dir>    convert every image in a directory, one level deep
imgconv                                  no arguments: the interactive terminal interface
```

### The graphical interface

`imgconv --ui` opens a page in your browser with the images in a folder, a format picker, quality and
resize controls, and a button that opens your system's own file dialog. It converts the same way the
command line does — same code, same guarantees — because a behaviour that differs between the two
would be a bug.

It listens on `127.0.0.1` only, and every request must carry a token generated fresh for that run, so
nothing else on your machine or your network can reach it. With no argument it browses under your home
directory; give it one (`imgconv --ui ~/Pictures`) to narrow that.

```bash
imgconv photo.jpg -o photo.png            # convert one file
imgconv photo.jpg --to webp               # error: webp is read-only here
imgconv photo.png -o small.jpg --resize 800x --quality 85
imgconv ~/Pictures --to jpeg --outdir ~/out --jobs 4
```

### Flags

| Flag | What it does |
|---|---|
| `-o` | write to this path; its extension picks the target format |
| `--to` | target format by name, e.g. `png` |
| `--outdir` | where a directory run writes (required for one) |
| `--force` | replace an existing output file |
| `--resize` | `WxH`, or `Wx` / `xH` to keep the aspect ratio |
| `--quality` | JPEG quality, 1–100; only for a jpeg target |
| `--jobs` | conversions in flight at once during a directory run (default: one per CPU) |
| `--max-pixels` | refuse an input whose header declares more pixels than this |

One of `-o` or `--to` is required — neither is ever guessed.

### Formats

| Format | Extensions | Read | Write |
|---|---|---|---|
| JPEG | `.jpg` `.jpeg` | ✅ | ✅ |
| PNG | `.png` | ✅ | ✅ |
| GIF | `.gif` | ✅ | ✅ |
| TIFF | `.tif` `.tiff` | ✅ | ✅ |
| BMP | `.bmp` | ✅ | ✅ |
| WebP | `.webp` | ✅ | ❌ |

WebP is decode-only because `golang.org/x/image` ships a decoder and no encoder. Asking for it as a
target is an error that says so, rather than a silent surprise.

**The extension is never trusted for input.** A `.png` that actually holds JPEG bytes is read as
JPEG, because the magic bytes decide. Extensions are only used to pick a *target*.

### Exit codes

The CLI is meant to be scriptable, so these are part of the interface:

| Code | Means |
|---|---|
| `0` | every requested conversion succeeded |
| `1` | a conversion failed — unreadable input, unsupported target, encode error |
| `2` | the invocation was wrong — bad flags, missing argument |

A directory run that partially failed exits non-zero **and names every file that failed** on stderr.

## What it will refuse to do

These are the reasons the project exists, and each one has tests that were watched failing before
they passed:

- **It will not overwrite an existing file** unless you pass `--force`.
- **It will not write over your input file, even with `--force`.** Output and input identity is
  settled on open file descriptors, so a symlink, a hard link, or `./a.jpg` versus `a.jpg` cannot
  fool it.
- **It will not leave a half-written file behind.** An encode that fails partway removes what it
  created; with `--force`, it writes to a temporary file and renames over the destination only after
  the encode is clean, so a failure leaves your previous output untouched.
- **It will not convert a folder where two files would land on the same output name.** It refuses the
  whole run and lists the collisions first, because there is no "intended" winner and `--force` does
  not pick one.
- **It will not decode an image bomb.** The header is checked before any pixel is allocated — both a
  pixel count and a byte budget — so a few kilobytes cannot ask for gigabytes. A resize is bounded
  the same way, because the derived side comes from the input's aspect ratio.
- **It will not silently drop frames.** Flattening an animated GIF to a still format says so, on
  every path — the CLI, the TUI and a directory run.

## Development

```bash
go test ./...          # the suite
go test -race ./...    # the directory run is concurrent; this is not optional
go vet ./...
gofmt -l . | tee /dev/stderr | (! read)     # gofmt -l alone exits 0 even when it finds problems
```

There is a fuzz target over the GIF frame parser, which is the one piece of hand-written parsing of
untrusted input:

```bash
go test -run '^$' -fuzz=FuzzIsAnimated -fuzztime=60s ./internal/imageio/go_test/
```

### Layout

```
cmd/imgconv/       flags, exit codes, CLI-or-TUI. No image logic
internal/imageio/  the only package that imports image/* — decode, encode, the format registry
internal/convert/  pure transforms over an image.Image. Touches no file
internal/pipeline/ the one place a single file is converted: decode → transform → encode
internal/batch/    the bounded parallel run over a directory
internal/tui/      the Bubble Tea model
```

The import arrow runs one way — `cmd → tui → batch → pipeline → convert → imageio` — and never back.
That is what makes the conversion core testable with no terminal and no temporary directory.

Every supported format is one row in one table in `internal/imageio`. The CLI's `--help`, the TUI's
format list and the extension guess all read it, so adding a format is adding a row.

[`AGENTS.md`](AGENTS.md) is the full contract, including the traps that have already been paid for.

## Dependencies

The standard library does the core formats. Three modules exist because it genuinely does not do
TIFF, WebP, or terminal UIs:

| Module | For | Licence |
|---|---|---|
| `golang.org/x/image` | TIFF, WebP decoding | BSD-3-Clause |
| `github.com/charmbracelet/bubbletea` | the TUI event loop | MIT |
| `github.com/charmbracelet/lipgloss` | the TUI's styling | MIT |

## Licence

[MIT](LICENSE).
