# Changelog

All notable changes to Transom. Versions follow [semantic versioning](https://semver.org).

## 0.2.3 — 2026-10-03

### Fixed

- Cleaning on Windows no longer appears to hang. The safety check before
  moving items to the Recycle Bin re-inspected every cache folder once per
  selected item and resolved the same folders again for every file it
  walked, so checking a few dozen cache items could take several minutes.
  It now inspects each category once per cleanup and resolves protected
  locations once per check; the same protections still apply.

## 0.2.2 — 2026-10-01

### Fixed

- Preserve VS Code user data in custom `--user-data-dir` locations by detecting
  its settings and workspace storage layout, even inside caches or temporary
  folders on macOS and Windows.

## 0.2.1 — 2026-10-01

### Fixed

- Preserve VS Code updater staging, application bundles and portable desktop
  installations when cleaning caches, temporary folders or old downloads.
- Protect editor settings, extensions, workspace state and unsaved-file
  backups on macOS and Windows, including relocated profiles and portable data.
- Recheck application protection immediately before trashing or deleting,
  including items from an earlier scan. Cache items must stay inside their
  platform's cache locations; bundled dependencies are never project junk.
- Preserve macOS application leftovers when ownership cannot be verified,
  and recheck installed applications before cleaning a previously orphaned entry.

## 0.2.0 — 2026-10-01

### Added

- Native Windows x64 and ARM64 desktop EXEs with the original embedded UI,
  app icon, light/dark/auto themes, preferences and cleanup history.
- Portable ZIPs, companion CLI EXEs, per-user Setup installers, Start Menu
  integration, uninstall support and Windows release builds.
- Windows app and package caches, logs, temp files, Visual Studio caches,
  native Recycle Bin scanning, recycling and explicit permanent emptying.
- Windows disk usage, File Explorer reveal, Windows project paths and
  AppData/system/junction protections. Windows app-leftover detection
  covers uninstalled Store/MSIX packages using native registration checks.

### Changed

- macOS-only scanning categories are adapted or omitted on Windows; macOS
  retains its original native window and scanning behavior.

## 0.1.1 — 2026-09-26

### Changed

- `transom ui` now opens the native `Transom.app` on macOS instead of a
  browser tab: the first run installs it into `/Applications` (falling
  back to `~/Applications` if that isn't writable), and later runs update
  it in place whenever the installed copy doesn't match the `transom`
  binary's version. A running copy is asked to quit before being updated.
  Pass `--browser` for the previous behavior — starting the panel's
  server and opening it in your default browser.
- App icon now matches the in-app logo.

## 0.1.0 — 2026-09-26

First release: a careful, read-only-until-you-say-so disk cleaner for macOS,
with a native app around an embedded control panel.

### Added

- **Scanning**, always read-only until you explicitly clean:
  - **System Junk** — app caches, logs, temporary files, Mail downloads,
    and the Trash.
  - **Developer** — Xcode DerivedData and Archives, unavailable simulators,
    package-manager caches (npm, Yarn, pnpm, Composer, pip, Go, Gradle,
    CocoaPods, Bun), Homebrew's cache, and Docker's dangling images and
    build cache.
  - **Projects** — stale `node_modules`, `vendor`, `.venv` and `target`
    folders under your configured project roots.
  - **Files** — large files, old Downloads, duplicate files (kept newest,
    the rest listed), and leftovers of uninstalled apps.
- **A control panel, not just a CLI** — one page per group with every
  item's path, size and last-modified time, a Reveal-in-Finder button, a
  running total and selection bar, a dry-run estimate before anything
  moves, history of past cleanups, settings for project folders and
  thresholds, and light/dark/auto themes.
- **Trash by default.** `transom clean` moves items to `~/.Trash`;
  permanent deletion needs an explicit `--delete` (CLI) or a separate
  opt-in and confirmation (panel).
- **A path guard** in front of every deletion: the resolved path must sit
  inside `$HOME` or an allowed temp directory, must not be `$HOME` itself
  or one of its standard top-level folders, must not be `/`, and must not
  contain `..`; symlinks are removed, never followed.
- **Risk levels** on every item — `safe`, `review`, `caution` — with only
  `safe` items pre-selected in the panel.
- **A fixed command allowlist** (`brew cleanup -s`, `xcrun simctl delete
  unavailable`, `docker system prune -f`, …) instead of shell strings.
- **Transom.app** on macOS: a plain AppKit + WKWebView window around the
  same panel, no Electron. `transom app install` puts it in
  `/Applications`; `transom app open` launches it.
- **Commands:** `scan`, `clean`, `ui`, `app install`, `app open`,
  `version`. Full CLI and HTTP contract in
  [`docs/CONTRACT.md`](docs/CONTRACT.md).
