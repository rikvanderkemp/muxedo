#!/usr/bin/env bash
# Capture CPU and heap profiles for internal/process benchmarks (no network).
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
out="${MUXEDO_PROF_OUT:-/tmp/muxedo-prof}"
mkdir -p "$out"
cd "$root"
go test -run='^$' -bench=. -benchmem -benchtime=2s \
	-cpuprofile="$out/cpu.prof" -memprofile="$out/mem.prof" ./internal/process/...
echo "Wrote $out/cpu.prof and $out/mem.prof"
echo "CPU:    go tool pprof -http=:0 $out/cpu.prof"
echo "alloc:  go tool pprof -http=:0 -sample_index=alloc_space $out/mem.prof"
