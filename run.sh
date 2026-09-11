#!/usr/bin/env sh
# Start imgconv's graphical interface.
#
# Double-clicked from a file manager, this script gets NO terminal. That matters:
# without a controlling terminal nothing sends SIGHUP when a window closes, so the
# server would be adopted by init and keep listening invisibly — a local server
# nobody can see and nobody remembers to stop. So the first thing this does is
# make sure it has a terminal, opening one if it has to.
#
# With a terminal, closing it or pressing ctrl+c stops the server. That is the
# whole contract.
set -eu
cd "$(dirname "$0")"

# IMGCONV_RELAUNCHED stops an infinite loop: if the terminal we opened somehow
# still reports no tty, we run anyway rather than spawning windows forever.
if [ ! -t 1 ] && [ -z "${IMGCONV_RELAUNCHED:-}" ]; then
  export IMGCONV_RELAUNCHED=1
  self="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"

  for term in konsole alacritty kitty foot wezterm gnome-terminal xfce4-terminal tilix terminator x-terminal-emulator xterm; do
    command -v "$term" >/dev/null 2>&1 || continue

    # 🔴 NOT "&". That is the whole reason this looks the way it does.
    #
    # POSIX: in a script, a command started with & has SIGINT and SIGQUIT set to
    # IGNORED — and that disposition is inherited. Backgrounding the terminal here
    # gave konsole an ignored SIGINT, which it passed to the second run.sh, which
    # passed it to imgconv. ctrl+c then did nothing at all, forever, and the window
    # looked hung. Confirmed by reading SigIgn in /proc: 0x6, which is SIGINT and
    # SIGQUIT together.
    #
    # "setsid --fork" forks on its own and returns immediately, so the terminal is
    # detached and this script still exits at once — without an & anywhere. The
    # desktop that launched us stops waiting for a window this script will never
    # map, and the terminal owns its own lifetime from then on.
    #
    # The flag that means "run this command" differs in every emulator, which is
    # the only reason this is a case and not one variable.
    # Nested, because the two questions are not independent: asking whether setsid
    # supports --fork only makes sense once setsid exists. Written flat, the second
    # answer overwrote the first and a machine WITHOUT setsid ended up trying to
    # run it — failing 127 with stderr on /dev/null, so a double-click did nothing
    # and said nothing.
    detach=""
    if command -v setsid >/dev/null 2>&1; then
      if setsid --help 2>&1 | grep -q -- "--fork"; then
        detach="setsid --fork"
      else
        detach="setsid"
      fi
    fi

    # With no setsid at all, BECOME the terminal instead. That costs the immediate
    # exit — the desktop goes back to waiting for a window this script will never
    # map — and it keeps ctrl+c working, which matters more. What it must never
    # become is "&": that sets SIGINT to ignored for everything downstream.
    if [ -z "$detach" ]; then
      case "$term" in
        gnome-terminal|tilix)  exec "$term" -- "$self" "$@" ;;
        wezterm)               exec "$term" start -- "$self" "$@" ;;
        kitty|foot)            exec "$term" "$self" "$@" ;;
        *)                     exec "$term" -e "$self" "$@" ;;
      esac
    fi

    case "$term" in
      gnome-terminal|tilix)  $detach "$term" -- "$self" "$@" >/dev/null 2>&1 ;;
      wezterm)               $detach "$term" start -- "$self" "$@" >/dev/null 2>&1 ;;
      kitty|foot)            $detach "$term" "$self" "$@" >/dev/null 2>&1 ;;
      *)                     $detach "$term" -e "$self" "$@" >/dev/null 2>&1 ;;
    esac
    exit 0
  done
  # No terminal emulator at all. Carry on rather than refusing: the URL still gets
  # printed, and ctrl+c is simply not available.
fi

if ! command -v go >/dev/null 2>&1; then
  echo "imgconv: Go is not installed."
  echo "Either install Go, or download a ready-made binary from"
  echo "  https://github.com/mateusands/imgconv/releases"
  [ -n "${IMGCONV_RELAUNCHED:-}" ] && { printf 'Press enter to close. '; read -r _; }
  exit 1
fi

# Rebuild every time. The interface is compiled into the binary with go:embed, so
# editing the page changes nothing until the binary is rebuilt, and a stale binary
# looks exactly like a change that did not work.
if ! go build -o imgconv ./cmd/imgconv; then
  echo "imgconv: the build failed."
  [ -n "${IMGCONV_RELAUNCHED:-}" ] && { printf 'Press enter to close. '; read -r _; }
  exit 1
fi

echo "Close this terminal to stop imgconv."
echo

# exec, so this shell is REPLACED by imgconv rather than waiting on it.
#
# It is what makes ctrl+c work the way the terminal window implies: the signal
# reaches imgconv directly and ends the process, and the window closes with it.
# Waiting here instead left the shell alive after the server died, sitting on a
# "press enter" prompt that ignored every further ctrl+c and looked like a hang.
#
# Anything that can fail with a message worth reading has already run above, and
# held the window itself.
exec ./imgconv --ui "$@"
