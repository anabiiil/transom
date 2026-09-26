<p align="center">
  <img src="assets/brand/transom-mark.svg" alt="Transom" width="72">
</p>

<h1 align="center">Transom</h1>

<p align="center">
  <strong>A careful disk cleaner for macOS.</strong><br>
  Finds caches, logs, developer junk and stale project dependencies, shows you exactly
  what it found, and only touches what you select.
</p>

```
transom scan     →  read-only report of what's reclaimable
transom clean    →  moves what you selected to the Trash
```

Transom is a sibling of Mullion, the local dev environment: one Go binary, an
embedded web control panel, and (on macOS) a native app window around it.

## Why Transom

- **Scanning never changes anything.** Every category is read-only until you
  explicitly choose to clean. Nothing is touched just by looking.
- **You decide, item by item.** The panel lists every cache folder, log,
  `node_modules`, large file and duplicate it found, with a size and a risk
  level, not just a single "clean everything" button.
- **Trash by default.** Cleaning moves items to `~/.Trash`, so a mistake is one
  Command-Z away in Finder. Permanent deletion is an explicit opt-in.
- **Guarded against the obvious disaster.** A path guard checks every item
  before it's touched: nothing outside your home folder or a safe temp
  directory, never `$HOME` itself or a top-level folder like Desktop or
  Documents, never a symlink target.

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
  and a Reveal-in-Finder button.
- A running total, a selection bar, and a dry-run estimate before anything
  is moved.
- History of past cleanups: what was freed, when, and how.
- Settings for project folders, the "stale" threshold, and the large-file
  threshold.
- Light, dark and auto themes.

**Native app (macOS)**
- `Transom.app`: a plain AppKit + WKWebView window around the same panel,
  no Electron. `transom app install` puts it in `/Applications`.

## Safety

These principles come straight from [`docs/CONTRACT.md`](docs/CONTRACT.md),
the internal spec this build follows:

1. **Scan is always read-only.** It only ever reads metadata; nothing is
   moved or deleted while scanning.
2. **Clean only acts on the latest scan.** The panel sends item IDs, never
   raw paths — the server resolves each ID against the stored scan result,
   and an unknown ID fails instead of falling back to guessing a path.
3. **Trash by default.** `transom clean` moves items to `~/.Trash`.
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

Download the latest release from
**[Releases](https://github.com/anabiiil/transom/releases/latest)**:
`Transom-<version>-macos.zip` unzips to `Transom.app` — drag it into
`/Applications`. It's ad-hoc signed (no notarization yet), so the first
launch needs **right-click → Open** instead of a double-click, or Gatekeeper
will refuse to open it.

Or run one command, without Homebrew — it installs the `transom` CLI to
`~/.transom/bin` and suggests `transom app install` to also put
`Transom.app` in `/Applications`:

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

Once the tap is published (it isn't yet — `anabiiil/homebrew-tap` doesn't
have a `transom` formula until the first tagged release runs the release
workflow):

```bash
brew tap anabiiil/tap
brew install transom
transom app install
```

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

## Commands

```
transom scan [--category id,...] [--json] [-v]     read-only report
transom clean [--category id,...] [--safe-only] [--delete] [--dry-run] [--yes]
transom ui [--window-host] [--detached] [--foreground]   open the control panel
transom app install [--dest dir]                    extract Transom.app into /Applications (macOS)
transom app open                                    open Transom.app (macOS)
transom version
```

Plain `transom ui` starts the panel's server and opens it in your default
browser, detaching from the terminal so it stays free (use `--foreground`
to keep it attached instead). The full HTTP contract the panel talks to
the server over — endpoints, request/response shapes, and the category
list — is documented in [`docs/CONTRACT.md`](docs/CONTRACT.md).
