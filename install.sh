#!/bin/sh
# Transom installer for macOS:
#   curl -fsSL https://raw.githubusercontent.com/anabiiil/transom/main/install.sh | sh
# Downloads the right binary for this Mac from the latest GitHub release
# and installs it, then suggests `transom app install`.
set -eu

REPO="anabiiil/transom"

case "$(uname -s)" in
  Darwin) ;;
  *) echo "This installer is for macOS. Transom is macOS-only."; exit 1 ;;
esac

case "$(uname -m)" in
  arm64)  ARCH="arm64" ;;
  x86_64) ARCH="amd64" ;;
  *) echo "Unsupported CPU architecture: $(uname -m)"; exit 1 ;;
esac

echo "Finding the latest release..."
TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
  grep '"tag_name"' | head -1 | cut -d'"' -f4)
if [ -z "$TAG" ]; then
  echo "Could not resolve the latest release — download it manually from:"
  echo "  https://github.com/$REPO/releases/latest"
  exit 1
fi

URL="https://github.com/$REPO/releases/download/$TAG/transom-$TAG-darwin-$ARCH.tar.gz"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "Downloading transom $TAG ($ARCH)..."
curl -fL --progress-bar "$URL" | tar -xz -C "$TMP"
chmod +x "$TMP/transom"
xattr -c "$TMP/transom" 2>/dev/null || true

BIN_DIR="$HOME/.transom/bin"
mkdir -p "$BIN_DIR"
cp "$TMP/transom" "$BIN_DIR/transom"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    echo
    echo "Add Transom to your PATH:"
    echo "  echo 'export PATH=\"\$HOME/.transom/bin:\$PATH\"' >> ~/.zshrc"
    echo "  (or ~/.bashrc, depending on your shell) then open a new terminal."
    ;;
esac

echo
echo "Installed transom $TAG to $BIN_DIR/transom"
echo "Install the native app with:"
echo "  $BIN_DIR/transom app install"
