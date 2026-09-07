# AGENTS.md — imgconv

> **The contract for any AI coding agent working in this repository.** `AGENTS.md` is the cross-vendor
> convention: one file, read by all of them, so there is exactly one copy of the rules.
>
> Nothing here depends on a particular tool being installed. If your setup adds its own procedures,
> put them in your own file and have it point at this one.

---

## What this project is

`imgconv` converts image files between formats — JPEG, PNG, GIF, TIFF, BMP and WebP — with optional
resizing and JPEG quality control, one file at a time or a whole directory in parallel. It is a
single binary with two front ends over one core: a flag-driven CLI for scripting, and a Bubble Tea
TUI for picking files interactively.

It is a **learning project.** It is written to learn Go, so *why* a thing is written a
particular way matters as much as *that it works*. Prefer the standard library and the idiomatic
construct over the clever one, and name the Go concept a piece of code is demonstrating when it is
not obvious.

**What it is not:** not a service, not a library published for others to import, not an editor. It
runs locally, on the operator's own files. It is developed on Linux; nothing about it is tied to a
platform, but nothing else has been tested.

---

## Source of truth

**The real state of the system is the code.** Never assume a format, a flag or a dependency exists:

| Question | The only file that answers it |
|---|---|
| Which formats are supported? | the registry table in `internal/imageio/format.go` |
| Which flags exist? | the flag block in `cmd/imgconv/config.go` |
| Which dependencies exist? | `go.mod` — `go list -m all` resolves them |

If anything in this document disagrees with the code, **the code wins** — and this file gets updated
in the same change.

---

## Stack

Read the versions from `go.mod`. Do not copy a version number into this file; it is the one fact here
that expires without announcing itself.

| Layer | Technology | Note |
|---|---|---|
| language / toolchain | Go | the version is `go.mod`'s `go` line |
| core formats | `image/jpeg`, `image/png`, `image/gif` (stdlib) | decode **and** encode, no dependency |
| extra formats | `golang.org/x/image` | TIFF and BMP both ways; WebP **decode only** |
| TUI | `charmbracelet/bubbletea` + `lipgloss` | the only place a terminal is touched |
| tests | `testing` (stdlib) | no test framework, no assertion library |

**The standard library is the default answer, and a new dependency is a decision, not a reflex.** In a
project whose purpose is learning Go, pulling in a module for every problem teaches the module. The
three above exist because the standard library genuinely does not do TIFF, WebP, or terminal UIs.

---

## Structure

```
cmd/imgconv/       the binary: flags, exit codes, and choosing CLI vs TUI. No image logic lives here
internal/imageio/  the only package that imports image/* — decode, encode, the format registry
internal/convert/  pure transforms over an image.Image (resize). Touches no file
internal/pipeline/ the ONE place a single file is converted: decode -> transform -> encode
internal/batch/    the goroutine fan-out over a directory, and its concurrency limit
internal/tui/      the Bubble Tea model. Renders and dispatches; decides nothing about images
internal/web/      the graphical front end: a page this binary serves to the browser
<pkg>/go_test/     the tests for <pkg>, in a package of their own
```

### The dependency arrow — the rule that matters more than the folders

```
cmd → {tui, web} → batch → pipeline → convert → imageio
```

**It runs one way and never back.** `imageio` imports nothing of ours. `convert` is pure and never
opens a file. `pipeline` is the only place the three steps appear together. **Nothing imports `tui`
except `cmd`.**

That is what makes the conversion core testable with no terminal and no temp directory: the package
that knows about formats has no idea a UI exists. A back-edge costs exactly that property, which is
the reason to notice one in review. `go list -deps ./internal/convert` must never mention `imageio`.

🔴 **`pipeline` exists because the three steps were once written three times** — in the CLI, in the
TUI and inside the directory run — and the copies had already drifted: two of them warned that an
animated GIF was being flattened and one did not, so converting a folder dropped frames in silence
while converting the same file alone said so. One decision, one home.

### Where the tests live

Tests are in a `go_test/` sub-package rather than beside the code. That is a deliberate choice and it
has a price: a test there is a **different Go package**, so it reaches only the exported API.
`internal/tui` exports `Model`, `NewModel`, `ListImages`, `TargetFormats` and `MoveCursor` for no
reason but that, and each says so in a comment.

**`cmd/imgconv` is the exception and cannot follow the rule.** Go refuses to import a `package main`
(*"is a program, not an importable package"*), so its tests stay beside it.

---

## Architecture

Both front ends call the same code. `cmd/imgconv` parses flags; with no arguments it starts the TUI,
otherwise it runs the conversion and exits with a status code. **The TUI does not have a second,
easier conversion path — if a behaviour differs between the two front ends, that is a bug, not a
feature of the TUI.**

A conversion is three steps and each belongs to exactly one package: `imageio` turns bytes into an
`image.Image`, `convert` applies transforms to that value, `imageio` writes it back out in the target
format. **The middle step never learns which format it came from** — that is deliberate, and it is
why resize needs no per-format code.

### The format contract — one place only

**Every format this tool supports is one row in one table in `internal/imageio`**, and that table is
the single source: the CLI's `--help`, the TUI's format list and the extension-to-format guess all
read from it. Adding a format means adding a row — a decoder, an encoder (or an explicit "decode
only"), and the extensions it answers to.

A format may have a decoder without an encoder; the reverse never happens. WebP is the live example,
and `Encode == nil` is what lets the error message say *"webp: decode only, cannot be a target
format"* instead of something vague.

**If you find yourself writing a `switch` on format name anywhere outside `imageio`, the abstraction
has leaked and that is the thing to fix.**

---

## Commands

```bash
go mod download                    # dependencies (go mod tidy to add or prune them)
go run ./cmd/imgconv               # run it — no arguments starts the TUI
go test ./...                      # test suite
go test -race ./...                # the batch package is concurrent; this is not optional
go vet ./...                       # static checks
gofmt -l .                         # lint: prints unformatted files, and EXITS 0 EITHER WAY
go build -o imgconv ./cmd/imgconv  # the binary
```

⚠️ **`gofmt -l .` exits 0 even when it finds unformatted files** — it reports by printing names, not
by its status code. The form with a real exit code is:

```bash
gofmt -l . | tee /dev/stderr | (! read)
```

A gate that checks `$?` on the plain command never fires.

---

## Development rules

- **Do not assume a format is supported** — verify it in the registry. Half the formats decode but do
  not encode, and that asymmetry is the whole reason the table exists.
- **The import arrow runs one way.** A back-edge makes the conversion core untestable without a
  terminal, which is the property the layering buys.
- **Every write to a user's image file goes through one function in `internal/imageio`** — that is
  where the "refuse to overwrite without `--force`" guarantee lives, and it is worth nothing if a
  second write path exists.
- 🔴 **The input file is opened read-only and is never written back to.** In-place conversion is not a
  feature; a converter that eats the original is the one failure here that cannot be undone.
- **Never let a failure surface without feedback.** Silent failure is the worst defect in this
  project. A batch that converted nine files and quietly skipped the tenth is the exact shape to
  prevent: every input is accounted for, and the run exits non-zero if any of them failed.
- **A long operation never runs where it blocks the interface.** In the TUI that means a `tea.Cmd`,
  never inside `Update`.
- **Every lipgloss style lives in one file in `internal/tui`** — no colour or width literal inline in
  a `View()`. A terminal UI has the same problem a web one does: styles scattered across renders stop
  being changeable.
- **Bound what you allocate before you allocate it.** Both the decode and the resize paths do this;
  see Security below for why the resize one is not optional.

### Comments

A comment costs a reader's attention every time the file is opened, forever. It earns that by saying
something the code does not, **at a different level than the code** — lower (a unit, a bound, what
zero means) or higher (what this is for, what it guarantees). A comment at the same level as its line
repeats the line, and gets deleted rather than improved.

- **On an interface** (a function, a type, an exported value): what it does, what it promises, what it
  demands of the caller. Not how it does it.
- **On an implementation** (a line, a block): why this way, what it guards against, what breaks
  without it. Never what the line already says.
- 🔴 **A stale comment is worse than no comment, because it is trusted.** If your change makes a
  nearby comment false, fixing it belongs in the same change — never in a follow-up.
- No emoji in code comments. A comment earns urgency by what it says.

---

## Non-negotiable: spec → behaviour → failing test

No production code is written without spec → behaviour → a test seen to fail.

1. **Spec — at the top of the test file.** A package-level comment above the first test saying what
   the contract is, **why it exists** (the bug or the decision that motivated it), and what is a rule
   rather than an implementation accident.
2. **Behaviour, not implementation.** `Test<Thing>_Should<result>When<condition>`, in the language of
   the operation rather than of the internals.
3. **Red → Green → Refactor, and Red is shown, not claimed.** Write the test, run it, **watch it
   fail**, and only then write the minimum that passes.

### What to test, by priority

| Priority | Target | Why |
|---|---|---|
| 🔴 High | integrity of the user's original files | irreversible damage |
| 🔴 High | extracted pure logic | it is what can genuinely be tested |
| 🔴 High | the error path | does the message REACH the user? |
| 🟡 Medium | rules extracted from the edge | |
| 🟢 Low | what a screen looks like | needs a human eye |

**Mocks for the external only.** Use a real temporary file rather than mocking the I/O layer — the
I/O behaviour *is* the contract here.

⚠️ **Green is not enough, and a green suite can prove nothing.** After green, exercise the real
binary. And the check that a test is real is to **break the implementation on purpose and watch the
test fail**: a test never seen failing is a test nobody has verified.

---

## Security context

- **No data is stored.** The images the operator feeds in are theirs; nothing is uploaded, logged in
  full, or copied anywhere but the output path they asked for. There is no network call, no
  telemetry, no config file, no cache directory.
- 🔴 **The output write path is the red zone.** It must not overwrite without `--force`, and it must
  never write over the input **even with** `--force`. Everything else here can be re-run; a source
  image written over cannot be recovered.
- 🔴 **`--ui` opens a listening socket, and that is the only network surface here.** It binds
  `127.0.0.1` and nothing else, every request must carry a token minted per run from `crypto/rand`,
  and a request announcing a foreign `Origin` is refused even with the token. Every file name that
  reaches a path goes through `Resolve`, which refuses a `..` segment outright, roots an absolute
  path at the current folder, and follows a symlink BEFORE testing containment. There is no directory
  browsing: the operator picks files in the system dialog, and the page never shows an absolute path.
  The threat is not a remote attacker; it is a page the operator has open in another tab, which
  cannot read our responses but can absolutely make us write.
- 🔴 **Every input file is hostile until decoded.** This tool's entire job is parsing untrusted binary
  files that someone else produced. The attack surface is decode bombs (a few KB that allocate
  gigabytes), dimensions that overflow when multiplied, and files whose extension lies about their
  contents. **Bound what you allocate before you allocate it, and never trust the extension over the
  magic bytes.**
- **Log paths and reasons, never content.** An error names the file and the failure — never pixel
  data, never EXIF.
- Secrets only in the environment. Never rewrite published git history.

---

## Traps already paid for

> The most valuable section of this document. Every line is time someone has already lost.
> Format: **the symptom** → **the real cause** → **the rule that avoids it**.

- **`--force` emptied the input file instead of refusing to touch it.** The write path opened the
  destination with `O_TRUNC` and compared it against the input *afterwards*. By the time the refusal
  ran, `photo.jpg` was already 0 bytes. → **Identity is settled on open file descriptors
  (`os.SameFile`) before anything can truncate.** The forced path deliberately omits `O_TRUNC`;
  truncation happens after the comparison, never before. A path-string comparison is not a
  substitute: `./a.jpg` and `a.jpg` are the same file, and a hard link or a symlink is the same file
  under a legitimately different name. See `internal/imageio/write.go`.

- **A green suite that proved nothing.** The write tests pass against a broken guard if they only
  assert `err != nil`. → **Every write test asserts the file's BYTES**, before and after. This was
  verified by reintroducing the bug on purpose and watching them fail.

- **The same operation, written three times, drifted.** The CLI, the TUI and the directory run each
  had their own decode-transform-encode. Two warned that an animated GIF was being flattened; the
  third said nothing, so a folder of animated GIFs lost every frame but the first in silence. → **One
  decision, one home** (`internal/pipeline`). When you find a duplicated rule, extract it; fixing both
  copies side by side guarantees the next caller is born wrong.

- **The decode guard bounded the input and nothing bounded the output.** A 76-byte PNG of 1×100
  pixels passes the pixel limit with four orders of magnitude to spare. Asked to `--resize 100x`, it
  produced 100×10000 — and the growth is quadratic, so 1×10000 with `--resize 10000x` asks
  `image.NewRGBA` for about 4 TB. **The operator names one side; the other is derived from the
  input's aspect ratio, which the file controls.** → **The limit lives where the allocation is**, and
  a zero limit means the default rather than "unlimited", so a caller that forgets is still bounded.

- **A hand-written parser over untrusted input is where loops become infinite.** `countGIFFrames`
  walks GIF blocks by lengths the file itself supplies. → It never decompresses, it stops at the
  second frame, and it is covered by a fuzz target. Run it after touching it:
  `go test -run '^$' -fuzz=FuzzIsAnimated -fuzztime=60s ./internal/imageio/go_test/`.

---

## Exit codes — a contract

The CLI is scriptable, so these are part of the interface:

| Code | Means |
|---|---|
| 0 | every requested conversion succeeded |
| 1 | a conversion failed (unreadable input, unsupported target, encode error) |
| 2 | the invocation was wrong (bad flags, missing argument) |

A batch that converted some files and failed on others exits non-zero **and names every file that
failed.** Errors go to stderr and name the file.

---

## Commits

Conventional Commits: `feat(scope): …`, `fix(scope): …`, `test(scope): …`, `refactor(scope): …`.
One commit per revertible unit — reverting one must not drag the others.

---

## If this file and the code disagree

**The code wins**, and this file gets corrected in the same change. Everything here is a claim about
`cmd/` and `internal/`; when a claim stops being true it is worse than nothing, because it is
believed.

**Every non-obvious decision carries a one-line reason where the decision lives** — the comment above
the guard, the header of the test file, the commit message. If the reason does not fit in one line,
the decision has not been made yet. That is the test that makes the rest of this document checkable:
not *"was this acceptable"*, which nobody can answer, but *"is the reason written"*, which anybody can.
