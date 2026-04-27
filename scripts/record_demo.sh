#!/usr/bin/env bash
# Record the local asciinema demo from docs/demo/session.toml.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

scenario="${MUXEDO_DEMO_SCENARIO:-docs/demo/session.toml}"
cast="${MUXEDO_DEMO_CAST:-docs/media/demo.cast}"
gif="${MUXEDO_DEMO_GIF:-docs/media/demo.gif}"
window_size="$(python3 scripts/demo_session.py --scenario "$scenario" --print-window-size)"

go build -o muxedo .
mkdir -p "$(dirname "$cast")"

asciinema rec --overwrite --window-size "$window_size" \
	-c "python3 scripts/demo_session.py --muxedo ./muxedo --scenario '$scenario' --require-ansi" \
	"$cast"

if command -v agg >/dev/null 2>&1; then
	agg "$cast" "$gif"
	echo "Wrote $cast and $gif"
else
	echo "Wrote $cast"
	echo "Install agg to also render $gif"
fi
