# Changelog

All notable changes to Transom. Versions follow [semantic versioning](https://semver.org).

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
