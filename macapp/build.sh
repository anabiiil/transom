#!/usr/bin/env bash
# Builds Transom.app (a plain AppKit/WKWebView shell, see Transom.swift) as a
# universal binary, self-contained with a universal `transom` Go binary in
# Contents/Resources, and packages it into
# internal/macapp/bundle/Transom.app.tar.gz, which the Go side embeds and
# installs into /Applications via `transom app install`. Runnable from any
# cwd; no Xcode project involved, just Go, swiftc, and the macOS command
# line tools.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

VERSION="${VERSION:-0.1.0-dev}"

BUNDLE_DIR="$REPO_ROOT/internal/macapp/bundle"
mkdir -p "$BUNDLE_DIR"
# Remove any tar.gz left over from a previous run BEFORE building the Go
# binary below. internal/macapp embeds this directory (go:embed all:bundle);
# if a stale tar.gz were still here, the Go binary we build next would embed
# it, and that binary would then be archived into the NEW tar.gz -- each
# rebuild doubling the previous one. Building the inner binary while bundle/
# holds only .gitkeep keeps it a plain, non-recursive, self-contained CLI.
rm -f "$BUNDLE_DIR"/*.tar.gz

BUILD_DIR="$(mktemp -d)"
trap 'rm -rf "$BUILD_DIR"' EXIT

APP_DIR="$BUILD_DIR/Transom.app"
CONTENTS_DIR="$APP_DIR/Contents"
mkdir -p "$CONTENTS_DIR/MacOS" "$CONTENTS_DIR/Resources"

echo "Building Transom.app $VERSION..."

# --- transom (Go) universal binary, embedded into Contents/Resources so
# the app bundle can locate and run it without any separate install.
echo "Building transom (Go) universal binary..."
LDFLAGS="-s -w -X transom/internal/version.Number=$VERSION"
(cd "$REPO_ROOT" && GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o "$BUILD_DIR/transom-arm64" .)
(cd "$REPO_ROOT" && GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$BUILD_DIR/transom-amd64" .)
lipo -create -output "$CONTENTS_DIR/Resources/transom" "$BUILD_DIR/transom-arm64" "$BUILD_DIR/transom-amd64"
chmod +x "$CONTENTS_DIR/Resources/transom"

# --- Transom (Swift shell) universal binary: compile each arch separately
# (swiftc doesn't take multiple -target flags at once) and glue them
# together with lipo.
# -runtime-compatibility-version none: without it, targeting macos12 pulls
# in Swift's pre-5.6 concurrency back-deployment shims, whose x86_64 slice
# newer Command Line Tools releases no longer ship (arm64/arm64e only),
# which fails the x86_64 link with "symbol(s) not found". We don't rely on
# any back-deployed runtime behavior, so disabling the compatibility shim
# entirely is safe.
swiftc -runtime-compatibility-version none -O -target arm64-apple-macos12 \
	-framework AppKit -framework WebKit \
	-o "$BUILD_DIR/Transom-arm64" "$SCRIPT_DIR/Transom.swift"
swiftc -runtime-compatibility-version none -O -target x86_64-apple-macos12 \
	-framework AppKit -framework WebKit \
	-o "$BUILD_DIR/Transom-x86_64" "$SCRIPT_DIR/Transom.swift"
lipo -create -output "$CONTENTS_DIR/MacOS/Transom" \
	"$BUILD_DIR/Transom-arm64" "$BUILD_DIR/Transom-x86_64"
chmod +x "$CONTENTS_DIR/MacOS/Transom"

sed "s/__VERSION__/$VERSION/g" "$SCRIPT_DIR/Info.plist" > "$CONTENTS_DIR/Info.plist"

# AppIcon.icns from the 1024x1024 brand source (mint/teal tile with a white
# sparkle mark, drawn on Apple's Big Sur icon grid): build the full iconset
# iconutil expects (each size plus its @2x, up through 512@2x) by
# downscaling it with sips.
ICONSET_DIR="$BUILD_DIR/AppIcon.iconset"
mkdir -p "$ICONSET_DIR"
SOURCE_ICON="$REPO_ROOT/assets/brand/app-icon-1024.png"
for spec in "16:icon_16x16.png" "32:icon_16x16@2x.png" "32:icon_32x32.png" "64:icon_32x32@2x.png" \
	"128:icon_128x128.png" "256:icon_128x128@2x.png" "256:icon_256x256.png" "512:icon_256x256@2x.png" \
	"512:icon_512x512.png" "1024:icon_512x512@2x.png"; do
	size="${spec%%:*}"
	name="${spec#*:}"
	sips -z "$size" "$size" "$SOURCE_ICON" --out "$ICONSET_DIR/$name" >/dev/null
done
iconutil -c icns "$ICONSET_DIR" -o "$CONTENTS_DIR/Resources/AppIcon.icns"

# Ad-hoc sign so Gatekeeper doesn't flag it as entirely unsigned; users still
# need to right-click > Open the first time since it's not notarized.
codesign --force --deep -s - "$APP_DIR"

OUT_FILE="$BUNDLE_DIR/Transom.app.tar.gz"
# COPYFILE_DISABLE: keep macOS tar from adding ._* AppleDouble files,
# which would break the bundle's code-signature seal once extracted.
COPYFILE_DISABLE=1 tar -czf "$OUT_FILE" -C "$BUILD_DIR" Transom.app
echo "Built $OUT_FILE"

# Also drop a plain copy at macapp/build/Transom.app for local
# testing/launching without going through `transom app install`.
FINAL_DIR="$REPO_ROOT/macapp/build"
rm -rf "$FINAL_DIR/Transom.app"
mkdir -p "$FINAL_DIR"
cp -R "$APP_DIR" "$FINAL_DIR/Transom.app"
echo "Built $FINAL_DIR/Transom.app"
