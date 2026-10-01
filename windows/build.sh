#!/usr/bin/env bash
# Build the portable Windows desktop + console executables from macOS,
# Linux, or Windows (Git Bash). Go and zip are the only required tools.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VERSION="${VERSION:-0.2.0}"
DIST_DIR="${DIST_DIR:-$REPO_ROOT/dist}"
ARCHES="${ARCHES:-amd64 arm64}"
PAYLOAD_DIR="$SCRIPT_DIR/setup/payload"

cd "$REPO_ROOT"
mkdir -p "$DIST_DIR"
DIST_DIR="$(cd "$DIST_DIR" && pwd)"

# This helper runs on the build host; the following builds target Windows.
# The generated .syso files contain the app icon, version information,
# Windows compatibility and per-monitor DPI manifests. No DLL sidecar is
# needed: go-webview2 embeds its architecture-specific runtime loader.
GOOS="$(go env GOHOSTOS)" GOARCH="$(go env GOHOSTARCH)" \
  go run github.com/tc-hib/go-winres@v0.3.3 make \
  --in windows/winres.json --out rsrc --arch amd64,arm64 \
  --file-version "$VERSION" --product-version "$VERSION"
cp rsrc_windows_amd64.syso rsrc_windows_arm64.syso "$SCRIPT_DIR/setup/"
mkdir -p "$PAYLOAD_DIR"
trap 'rm -f "$PAYLOAD_DIR/Transom.exe" "$PAYLOAD_DIR/transom-cli.exe" "$PAYLOAD_DIR/Uninstall.exe" "$PAYLOAD_DIR/README.md" "$PAYLOAD_DIR/LICENSE" "$PAYLOAD_DIR/THIRD_PARTY_NOTICES.txt"' EXIT

for arch in $ARCHES; do
  case "$arch" in
    amd64|arm64) ;;
    *) echo "Unsupported Windows architecture: $arch" >&2; exit 1 ;;
  esac
  package_dir="$DIST_DIR/windows-$arch"
  mkdir -p "$package_dir"
  common_flags="-s -w -X transom/internal/version.Number=$VERSION"
  echo "Building Transom $VERSION for Windows/$arch..."
  GOOS=windows GOARCH="$arch" CGO_ENABLED=0 go build -trimpath \
    -ldflags "$common_flags -H=windowsgui -X transom/cmd.guiBuild=true" \
    -o "$package_dir/Transom.exe" .
  GOOS=windows GOARCH="$arch" CGO_ENABLED=0 go build -trimpath \
    -ldflags "$common_flags" -o "$package_dir/transom-cli.exe" .
  if [[ -f "$SCRIPT_DIR/README.md" ]]; then
    cp "$SCRIPT_DIR/README.md" "$package_dir/README.md"
  else
    cp README.md "$package_dir/README.md"
  fi
  cp "$REPO_ROOT/LICENSE" "$package_dir/LICENSE"
  cp "$SCRIPT_DIR/THIRD_PARTY_NOTICES.txt" "$package_dir/THIRD_PARTY_NOTICES.txt"
  cp "$package_dir/README.md" "$PAYLOAD_DIR/README.md"
  cp "$package_dir/LICENSE" "$PAYLOAD_DIR/LICENSE"
  cp "$package_dir/THIRD_PARTY_NOTICES.txt" "$PAYLOAD_DIR/THIRD_PARTY_NOTICES.txt"
  GOOS=windows GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -tags uninstaller \
    -ldflags "$common_flags -H=windowsgui" \
    -o "$PAYLOAD_DIR/Uninstall.exe" ./windows/setup
  cp "$package_dir/Transom.exe" "$PAYLOAD_DIR/Transom.exe"
  cp "$package_dir/transom-cli.exe" "$PAYLOAD_DIR/transom-cli.exe"
  setup_exe="$DIST_DIR/Transom-${VERSION}-Setup-${arch}.exe"
  GOOS=windows GOARCH="$arch" CGO_ENABLED=0 go build -trimpath \
    -ldflags "$common_flags -H=windowsgui" -o "$setup_exe" ./windows/setup
  archive="$DIST_DIR/transom_${VERSION}_windows_${arch}.zip"
  rm -f "$archive"
  (cd "$package_dir" && zip -q -r "$archive" .)
  echo "Built $package_dir/Transom.exe"
  echo "Built $setup_exe"
  echo "Packaged $archive"
done
