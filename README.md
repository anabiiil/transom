<p align="center">
  <img src="assets/brand/transom-mark.svg" alt="Transom" width="72">
</p>

<h1 align="center">Transom</h1>

<p align="center">
  <strong>A careful disk cleaner for macOS and Windows.</strong><br>
  Finds caches, logs, developer junk and stale project dependencies, shows you exactly
  what it found, and only touches what you select.
</p>

```
transom scan     →  read-only report of what's reclaimable
transom clean    →  moves what you selected to the Trash
```

Transom is a sibling of Mullion, the local dev environment: one Go binary, an
embedded web control panel, and a native app window on macOS and Windows.

## Why Transom

- **Scanning never changes anything.** Every category is read-only until you
  explicitly choose to clean. Nothing is touched just by looking.
- **You decide, item by item.** The panel lists every cache folder, log,
  `node_modules`, large file and duplicate it found, with a size and a risk
  level, not just a single "clean everything" button.
- **Trash by default.** Cleaning moves items to `~/.Trash`, so a mistake is one
  restore away in Finder. Windows uses the native Recycle Bin with original-location
  restore information. Permanent deletion is an explicit opt-in.
- **Guarded against the obvious disaster.** A path guard checks every item
  before it's touched: nothing outside your home folder or a safe temp
  directory, never `$HOME` itself or a top-level folder like Desktop or
  Documents, never a symlink target.
- **Applications and editor data are protected.** Cache cleanup keeps app
  installations, updater staging, VS Code settings, extensions and unsaved-file
  backups. The same protection applies to portable editors and old scan results
  on macOS and Windows.

## Features

**Scanning**
- **System Junk** — app caches, logs, temporary files, Mail downloads, and
  the Trash.
- **Developer** — Xcode DerivedData and Archives, unavailable simulators,
  package-manager caches (npm, Yarn, pnpm, Composer, pip, Go, Gradle,
  CocoaPods, Bun), Homebrew's cache, and Docker's dangling images and
  build cache.
- **Projects** — `node_modules`, `vendor`, `.venv` and `target` folders in
  projects you haven't touched in a while, searched under your configured
  project roots.
- **Files** — large files, files sitting in Downloads for months,
  duplicate files (kept newest, the rest listed), and leftovers of
  uninstalled apps.

**A control panel, not just a CLI**
- One page per group, with every item's path, size and last-modified time,
  and a Reveal button that opens Finder or File Explorer.
- A running total, a selection bar, and a dry-run estimate before anything
  is moved.
- History of past cleanups: what was freed, when, and how.
- Settings for project folders, the "stale" threshold, and the large-file
  threshold.
- Light, dark and auto themes.

**Native app (macOS)**
- `Transom.app`: a plain AppKit + WKWebView window around the same panel,
  no Electron. `transom ui` installs it into `/Applications` (or updates
  it there) and opens it — no separate install step needed.

**Native app (Windows)**

- `Transom.exe`: a native WebView2 window around the original panel, with
  the same design, pages, icon, themes, selections, settings and history.
  The desktop EXE opens without a console; `transom-cli.exe` provides the CLI.
- x64 and ARM64 Setup installers and portable ZIPs. Cleanup uses Windows
  cache paths, File Explorer and the Recycle Bin. Visual Studio and NuGet
  replace relevant Mac tooling; Xcode, Homebrew and Apple Mail categories
  are omitted. App-leftover detection covers uninstalled Store/MSIX packages;
  generic desktop-app data is kept.

## Safety

These principles come straight from [`docs/CONTRACT.md`](docs/CONTRACT.md),
the internal spec this build follows:

1. **Scan is always read-only.** It only reads file contents and metadata; nothing is
   moved or deleted while scanning.
2. **Clean only acts on the latest scan.** The panel sends item IDs, never
   raw paths — the server resolves each ID against the stored scan result,
   and an unknown ID fails instead of falling back to guessing a path.
3. **Trash by default.** `transom clean` moves items to `~/.Trash` on macOS
   or the native Recycle Bin on Windows.
   Permanent deletion needs an explicit `--delete` (CLI) or a separate
   opt-in and confirmation (panel).
4. **A path guard runs before anything is touched.** The resolved path
   (after resolving symlinks in its parent) must be inside `$HOME` or an
   allowed temp directory, must not be `$HOME` itself or one of its
   standard top-level folders (Desktop, Documents, Downloads, Library,
   Pictures, Movies, Music, Applications, Public), must not be `/`, and
   must not contain `..`. Deletion never follows a symlink — it removes
   the link itself, not whatever it points to.
5. **Every item carries a risk level.** `safe` (regenerated automatically:
   caches, logs, DerivedData), `review` (probably unwanted, but check
   first: stale dependencies, old downloads, app leftovers), or `caution`
   (your own files: large files, duplicates). Only `safe` items are
   pre-selected in the panel.
6. **Project roots are explicit.** Stale-dependency scanning only looks
   inside the folders you've configured (default: your home folder,
   excluding `~/Library`) — set from the Settings page or `--root`.
7. **Commands run from a fixed allowlist**, never a shell string — things
   like `brew cleanup -s` or `xcrun simctl delete unavailable` are exact,
   hardcoded argv, not interpolated text.

## Install

### Windows

Download **v0.2.0** for your processor:

| Windows PC | Installer | Portable ZIP |
| --- | --- | --- |
| x64 / Intel / AMD | [Download x64 Setup](https://github.com/anabiiil/transom/releases/download/v0.2.0/Transom-0.2.0-Setup-amd64.exe) | [Download x64 ZIP](https://github.com/anabiiil/transom/releases/download/v0.2.0/transom_0.2.0_windows_amd64.zip) |
| ARM64 | [Download ARM64 Setup](https://github.com/anabiiil/transom/releases/download/v0.2.0/Transom-0.2.0-Setup-arm64.exe) | [Download ARM64 ZIP](https://github.com/anabiiil/transom/releases/download/v0.2.0/transom_0.2.0_windows_arm64.zip) |

Run the Setup EXE to install for your current account, create a Start Menu
shortcut and add Transom to Windows' installed-apps list. Administrator access
is not required. For portable use, extract the ZIP and double-click
`Transom.exe`. Both options include the console companion `transom-cli.exe`.

The Windows desktop uses the original interface in a native WebView2 window.
It needs **Windows 10 or 11, x64 or ARM64**, and the
[Microsoft Edge WebView2 Evergreen Runtime](https://developer.microsoft.com/microsoft-edge/webview2/).
If the runtime is missing, install it from Microsoft and open Transom again.
No separate Go, Node, Python or .NET installation is needed.
See [`windows/README.md`](windows/README.md) for Windows categories, safe cleanup,
storage locations and builds. Windows uses its own cache paths, File Explorer
and Recycle Bin; macOS-only categories are replaced or omitted.

### macOS

Download the latest release from
**[Releases](https://github.com/anabiiil/transom/releases/latest)**:
`Transom-<version>-macos.zip` unzips to `Transom.app` — drag it into
`/Applications`. It's ad-hoc signed (no notarization yet), so the first
launch needs **right-click → Open** instead of a double-click, or Gatekeeper
will refuse to open it.

Or run one command, without Homebrew — it installs the prebuilt `transom`
CLI to `~/.transom/bin`. Run `transom ui` to install `Transom.app` in
`/Applications` and open it. No Xcode or Command Line Tools update is
needed to install the release:

```bash
curl -fsSL https://raw.githubusercontent.com/anabiiil/transom/main/install.sh | sh
```

Or download `transom-<version>-darwin-arm64.tar.gz` (Apple Silicon) or
`-darwin-amd64.tar.gz` (Intel) from Releases, then:

```bash
tar -xzf transom-*-darwin-*.tar.gz
xattr -c ./transom   # clear the quarantine flag Gatekeeper puts on downloads
./transom app install
```

### Homebrew

Install the prebuilt cask. The installer handles tap setup and trusts only
the Transom cask on Homebrew versions that require it:

```bash
curl -fsSL https://raw.githubusercontent.com/anabiiil/transom/main/install.sh | sh -s -- --homebrew
transom ui
```

Or run the Homebrew steps directly:

```bash
brew tap anabiiil/tap
brew update
if brew help trust >/dev/null 2>&1; then
  brew trust --cask anabiiil/tap/transom
fi
brew install --cask anabiiil/tap/transom
transom ui   # first run installs Transom.app into /Applications
```

The cask downloads the released binary and does not build from source, so
it avoids Homebrew's formula build checks for outdated Command Line Tools.
If you installed the previous formula, switch once with
`brew uninstall --formula transom`, then install the cask using the steps
above. Subsequent updates use `brew upgrade --cask anabiiil/tap/transom`.
The release is ad-hoc signed. If macOS blocks `transom` on its first launch,
allow it in **System Settings → Privacy & Security**.

### Build from source

You'll need Go (see `go.mod`) and, for the native app, Xcode's command line
tools.

From a checkout of this repository:

```bash
go build -o transom .
./transom scan
```

#### The native macOS app

```bash
bash macapp/build.sh      # builds Transom.app as a universal binary,
                           # packages it into internal/macapp/bundle/
go build -o transom .      # rebuild so the Go binary embeds the new bundle
./transom app install      # extracts Transom.app into /Applications
./transom app open         # or just launch it from Applications/Spotlight
```

`macapp/build.sh` compiles `macapp/Transom.swift` (a plain AppKit + WKWebView
shell, no Xcode project needed) for both architectures with `swiftc`, glues
them together with `lipo`, ad-hoc signs the bundle, and also drops a plain
copy at `macapp/build/Transom.app` for local testing.

#### Tests

```bash
go test ./...
```

#### Windows builds

```bash
VERSION=0.2.0 bash windows/build.sh
```

Or on Windows:

```powershell
powershell -ExecutionPolicy Bypass -File windows\build.ps1 -Version 0.2.0
```

Both build x64 and ARM64 desktop EXEs, CLI EXEs, Setup installers and portable
ZIP packages under `dist/`, with the original icon and Windows DPI manifest.

## Commands

```
transom scan [--category id,...] [--json] [-v]     read-only report
transom clean [--category id,...] [--safe-only] [--delete] [--dry-run] [--yes]
transom ui [--browser] [--foreground]                open the control panel
transom app install [--dest dir]                    extract Transom.app into /Applications (macOS)
transom app open                                    open Transom.app (macOS)
transom version
```

On macOS, `transom ui` opens the native `Transom.app`: the first run installs
it into `/Applications` (falling back to `~/Applications` if that isn't
writable), and later runs update it in place whenever the installed copy is
older than the `transom` binary you're running. Pass `--browser` for the old
behavior instead — starting the panel's server and opening it in your
default browser, detaching from the terminal so it stays free (use
`--foreground` to keep it attached instead). Windows opens a native desktop
window by default; `--browser` opens a browser instead. The full HTTP contract
the panel talks to the server over —
endpoints, request/response shapes, and the category list — is documented
in [`docs/CONTRACT.md`](docs/CONTRACT.md).
