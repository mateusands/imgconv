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
    # The flag that means "run this command" is different in every one of them,
    # which is the only reason this is a case and not a loop with one variable.
    case "$term" in
      gnome-terminal|tilix)  exec "$term" -- "$self" "$@" ;;
      wezterm)               exec "$term" start -- "$self" "$@" ;;
      kitty|foot)            exec "$term" "$self" "$@" ;;
      *)                     exec "$term" -e "$self" "$@" ;;
    esac
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

echo "Close this window or press ctrl+c to stop imgconv."
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
