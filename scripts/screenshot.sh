#!/usr/bin/env bash
# Screenshots a page in headless Chrome, so checking the web app opens no window on the user's screen.
# The page loads in an iframe of the requested size: headless Chrome's window is never narrower than 500px.
# A worktree session can run this as a plain command; its guard refuses Chrome's path, which has spaces in it.
# Usage: scripts/screenshot.sh <url> <width> <light|dark> <out.png> [height]
set -euo pipefail

die() { echo "screenshot.sh: $*" >&2; exit 1; }

[[ $# -ge 4 && $# -le 5 ]] || die "usage: <url> <width> <light|dark> <out.png> [height]"
url="$1" width="$2" scheme="$3" out="$4" height="${5:-900}"
chrome="${CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"

# Blink's PreferredColorScheme: 0 is dark, 1 is light. Without it Chrome follows the Mac's appearance.
case "$scheme" in
  light) preferred=1 ;;
  dark) preferred=0 ;;
  *) die "scheme is light or dark, not $scheme" ;;
esac

frame="$(mktemp -d)"
trap 'rm -rf "$frame"' EXIT
# Centered, because sips ignores --cropOffset and always crops around the middle.
printf '<!doctype html><style>html,body{margin:0}iframe{border:0;display:block;margin:0 auto}</style><iframe src="%s" width="%s" height="%s"></iframe>\n' \
  "$url" "$width" "$height" >"$frame/frame.html"

rm -f "$out"
"$chrome" --headless --disable-gpu --hide-scrollbars --blink-settings="preferredColorScheme=$preferred" \
  --virtual-time-budget=5000 --window-size="$((width < 500 ? 500 : width)),$height" \
  --screenshot="$out" "file://$frame/frame.html" 2>/dev/null || true
[[ -s $out ]] || die "Chrome wrote no screenshot"
if [[ $width -lt 500 ]]; then sips --cropToHeightWidth "$height" "$width" "$out" >/dev/null; fi
echo "$out"
