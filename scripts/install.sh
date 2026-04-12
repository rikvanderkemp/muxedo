#!/bin/sh
set -eu

OWNER="rikvanderkemp"
REPO="muxedo"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${VERSION:-latest}"
TMPDIR_ROOT="${TMPDIR:-/tmp}"
metadata_file=""
archive_file=""
checksums_file=""
install_file=""

say() {
	printf '%s\n' "$*"
}

fail() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

detect_os() {
	case "$(uname -s)" in
		Linux) printf 'linux\n' ;;
		Darwin) printf 'darwin\n' ;;
		*) fail "unsupported operating system: $(uname -s)" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
		x86_64|amd64) printf 'amd64\n' ;;
		aarch64|arm64) printf 'arm64\n' ;;
		*) fail "unsupported architecture: $(uname -m)" ;;
	esac
}

github_api_base() {
	printf 'https://api.github.com/repos/%s/%s/releases' "$OWNER" "$REPO"
}

release_json() {
	if [ "$VERSION" = "latest" ]; then
		printf '%s/latest\n' "$(github_api_base)"
	else
		printf '%s/tags/%s\n' "$(github_api_base)" "$VERSION"
	fi
}

extract_json_string() {
	key="$1"
	sed -n "s/.*\"$key\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p" | head -n 1
}

checksum_cmd() {
	if command -v sha256sum >/dev/null 2>&1; then
		printf 'sha256sum\n'
	elif command -v shasum >/dev/null 2>&1; then
		printf 'shasum -a 256\n'
	elif command -v openssl >/dev/null 2>&1; then
		printf 'openssl dgst -sha256\n'
	else
		fail "missing checksum tool: need sha256sum, shasum, or openssl"
	fi
}

compute_sha256() {
	file="$1"
	cmd="$(checksum_cmd)"
	case "$cmd" in
		sha256sum)
			sha256sum "$file" | awk '{print $1}'
			;;
		"shasum -a 256")
			shasum -a 256 "$file" | awk '{print $1}'
			;;
		"openssl dgst -sha256")
			openssl dgst -sha256 "$file" | awk '{print $NF}'
			;;
	esac
}

path_has_dir() {
	dir="$1"
	old_ifs=$IFS
	IFS=:
	for entry in $PATH; do
		if [ "$entry" = "$dir" ]; then
			IFS=$old_ifs
			return 0
		fi
	done
	IFS=$old_ifs
	return 1
}

cleanup() {
	rm -f "${metadata_file:-}" "${archive_file:-}" "${checksums_file:-}" "${install_file:-}"
}

need_cmd curl
need_cmd tar
need_cmd mv
need_cmd chmod
need_cmd mkdir
need_cmd mktemp

os="$(detect_os)"
arch="$(detect_arch)"

metadata_url="$(release_json)"
metadata_file="$(mktemp "$TMPDIR_ROOT/muxedo-release.XXXXXX")"
trap cleanup EXIT INT TERM HUP

http_code="$(curl -fsSL -H 'Accept: application/vnd.github+json' -o "$metadata_file" -w '%{http_code}' "$metadata_url" || true)"
if [ "$http_code" != "200" ]; then
	if [ "$VERSION" = "latest" ]; then
		fail "failed to fetch latest release metadata"
	fi
	fail "failed to fetch release metadata for $VERSION"
fi

tag_name="$(extract_json_string tag_name <"$metadata_file")"
[ -n "$tag_name" ] || fail "release metadata missing tag_name"

version_no_v="${tag_name#v}"
archive_name="muxedo_${version_no_v}_${os}_${arch}.tar.gz"
archive_url="https://github.com/${OWNER}/${REPO}/releases/download/${tag_name}/${archive_name}"
checksums_url="https://github.com/${OWNER}/${REPO}/releases/download/${tag_name}/checksums.txt"

archive_file="$(mktemp "$TMPDIR_ROOT/muxedo-archive.XXXXXX")"
checksums_file="$(mktemp "$TMPDIR_ROOT/muxedo-checksums.XXXXXX")"
install_file="$(mktemp "$TMPDIR_ROOT/muxedo-bin.XXXXXX")"

say "Installing muxedo ${tag_name} for ${os}/${arch}"

curl -fsSL -o "$archive_file" "$archive_url" || fail "failed to download ${archive_name}"
curl -fsSL -o "$checksums_file" "$checksums_url" || fail "failed to download checksums.txt"

expected_sum="$(awk -v name="$archive_name" '$2 == name || $2 == "*"name { print $1; exit }' "$checksums_file")"
[ -n "$expected_sum" ] || fail "missing checksum for ${archive_name}"

actual_sum="$(compute_sha256 "$archive_file")"
[ "$actual_sum" = "$expected_sum" ] || fail "checksum mismatch for ${archive_name}"

tar -xzf "$archive_file" -O muxedo >"$install_file" || fail "failed to extract muxedo from ${archive_name}"
chmod 755 "$install_file"

mkdir -p "$INSTALL_DIR"
target="$INSTALL_DIR/muxedo"
mv "$install_file" "$target" || fail "failed to install muxedo to ${target}"

say "Installed to $target"
say
say "Run: muxedo -version"

if ! path_has_dir "$INSTALL_DIR"; then
	say
	say "$INSTALL_DIR not in PATH. Add:"
	say "  export PATH=\"$INSTALL_DIR:\$PATH\""
fi
