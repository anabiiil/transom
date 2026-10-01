# Transom — internal contract

Transom is a macOS and Windows disk cleaner, a sibling of Mullion (`../mullion`): one Go binary
(cobra CLI, module `transom`), an embedded web control panel served on 127.0.0.1,
and a native `Transom.app` (AppKit + WKWebView) that hosts the panel.

Windows uses the same embedded panel inside a native WebView2 window,
distributed as `Transom.exe` (GUI), `transom-cli.exe` (console), a portable
ZIP and a per-user Setup EXE. Closing its native window stops the loopback
server and cancels active scans. The platform sections below override
macOS-specific paths and commands; the JSON API is unchanged.

## Safety principles (non-negotiable)

1. **Scan never modifies anything.** Scanning is read-only.
2. **Clean only acts on items from the latest scan.** The client sends item IDs, never
   raw paths. The server resolves IDs against the stored scan result; unknown IDs fail.
3. **Default mode is "trash"** (move to `~/.Trash`, recoverable). Permanent delete is an
   explicit opt-in (`mode: "delete"`).
4. **Path guard** before touching anything: the resolved path (after `filepath.EvalSymlinks`
   of its parent) must be inside `$HOME` or an allowed temp root (`os.TempDir()`,
   `/private/var/folders/<user tmp>`), must not be `$HOME` itself or any of its direct
   children that are standard folders (Desktop, Documents, Downloads, Library, Pictures,
   Movies, Music, Applications, Public), must not be `/`, and must not contain `..`.
   Never follow symlinks while deleting (remove the link, not the target).
5. **Risk levels** on every item: `safe` (regenerated automatically: caches, logs,
   DerivedData), `review` (probably unwanted but user data-ish: stale node_modules,
   old downloads, leftovers), `caution` (user files: large files, duplicates).
   Only `safe` items are pre-selected in the UI.
6. **Command items** (e.g. `brew cleanup`, `xcrun simctl delete unavailable`,
   `docker system prune`) are run only via an allowlist of fixed argv — never a shell string.

## CLI

```
transom scan [--category id,...] [--json]     read-only report
transom clean [--category id,...] [--safe-only] [--delete] [--dry-run] [--yes]
transom ui [--window-host] [--detached]       open the control panel
transom app install                           extract Transom.app into /Applications (macOS)
transom version
```

`transom ui --window-host` (used by Transom.app): starts the server, prints exactly one line
`TRANSOM_UI_URL=http://127.0.0.1:<port>/?t=<token>` to stdout, then blocks until stdin is
closed (parent app exited) or SIGTERM.

Plain `transom ui`: starts the server and opens the URL in the default browser
(detaches on macOS like Mullion unless `--foreground`).

## HTTP API

- All API calls: `POST`, JSON body, header `X-Transom-Token: <token>`.
- Response: `{"ok": true, "data": ...}` or `{"ok": false, "error": "message"}`.
- `GET /` serves `internal/ui/web/index.html`; other static files from `internal/ui/web/`
  at `/static/<name>` (e.g. `/static/app.js`, `/static/app.css`, `/static/favicon.png`).
- The token is read by the page from the `t` query param of its own URL.

| Endpoint | Request | Response `data` |
|---|---|---|
| `/api/disk` | `{}` | `{"total": int64, "free": int64, "used": int64, "volume": "Macintosh HD"}` |
| `/api/categories` | `{}` | `[Category]` |
| `/api/scan/start` | `{"categories": ["id"...] (empty = all), "options": ScanOptions}` | `{"jobId": "s1"}` |
| `/api/scan/status` | `{"jobId": "s1"}` | `ScanStatus` |
| `/api/scan/cancel` | `{"jobId": "s1"}` | `{}` |
| `/api/clean` | `{"items": ["itemId"...], "mode": "trash"\|"delete", "dryRun": bool}` | `CleanResult` |
| `/api/reveal` | `{"path": "/abs/path"}` (must be an item path from the last scan) | `{}` — opens Finder (`open -R`) |
| `/api/history` | `{}` | `[HistoryEntry]` newest first (max 50) |
| `/api/prefs/get` | `{}` | `{"key": "value", ...}` |
| `/api/prefs/set` | `{"key": "k", "value": "v"}` | `{}` |

### Types (JSON field names are exact)

```
Category    { id, name, group, description, risk, icon }
            group ∈ "system" | "developer" | "projects" | "files"
            risk  ∈ "safe" | "review" | "caution"
            icon: short keyword ("cache","log","trash","xcode","node","docker","box","file","copy","app")

ScanOptions { staleDays: int (default 60), largeMinMB: int (default 500),
              roots: [string] (project roots for node_modules/vendor; default ~/ excluding ~/Library) }

ScanStatus  { state: "running"|"done"|"error"|"cancelled",
              progress: { category: string, scanned: int, found: int, elapsedMs: int },
              error?: string,
              result?: ScanResult }        // present when state == "done"

ScanResult  { scannedAt: RFC3339, totalSize: int64,
              categories: [ { id, name, group, risk, description, icon,
                              totalSize: int64, count: int, items: [Item] } ] }

Item        { id: string (stable within a scan), path: string, label: string,
              size: int64, modTime: RFC3339, kind: "dir"|"file"|"command",
              risk, note?: string, group?: string }   // group: duplicate-set id for duplicates

CleanResult { freed: int64, removed: int, dryRun: bool,
              failed: [ { id, path, error } ] }

HistoryEntry { at: RFC3339, freed: int64, removed: int, mode, categories: [string] }
```

Sizes are bytes. The UI formats them (base 1000, like Finder).

## Categories (ids)

| id | group | risk | what |
|---|---|---|---|
| `user-caches` | system | safe | `~/Library/Caches/*` (one item per app folder) |
| `user-logs` | system | safe | `~/Library/Logs/*`, `~/Library/Logs/DiagnosticReports` |
| `temp-files` | system | safe | `$TMPDIR` entries older than 1 day |
| `trash` | system | review | `~/.Trash` contents (clean = permanent delete) |
| `mail-downloads` | system | safe | `~/Library/Containers/com.apple.mail/Data/Library/Mail Downloads` |
| `xcode` | developer | safe | DerivedData, Archives (review), iOS DeviceSupport, `CoreSimulator/Caches` |
| `simulators` | developer | safe | command: `xcrun simctl delete unavailable` (size = sum of unavailable devices) |
| `package-caches` | developer | safe | npm (`~/.npm/_cacache`), yarn, pnpm store, composer, pip, go build cache, gradle, cocoapods, bun |
| `homebrew` | developer | safe | command: `brew cleanup -s` + `~/Library/Caches/Homebrew` |
| `docker` | developer | review | command: `docker system prune -f` (only if docker is installed) |
| `stale-deps` | projects | review | `node_modules` / `vendor` / `.venv` / `target` in projects untouched for `staleDays` |
| `large-files` | files | caution | files ≥ `largeMinMB` under ~ (excluding ~/Library, dependency dirs) |
| `old-downloads` | files | review | `~/Downloads` items older than 90 days |
| `duplicates` | files | caution | identical files ≥ 1 MB in Desktop/Documents/Downloads (size → partial hash → full hash); keep newest, list the others |
| `app-leftovers` | files | review | `~/Library/{Application Support,Caches,Containers,Preferences,Saved Application State}` entries whose bundle id / name matches no installed app in /Applications, ~/Applications, /System/Applications |

## Windows adaptations

The page carries `data-platform="windows"`; the frontend keeps its original
markup/styles and uses Windows labels and path validation. `/api/disk` uses
native volume APIs for the home volume. `/api/reveal` opens File Explorer
through `SHOpenFolderAndSelectItems`, after the same latest-scan check.

Windows keeps `user-caches`, `user-logs`, `temp-files`, `trash`, `package-caches`,
`docker`, `stale-deps`, `large-files`, `old-downloads`, `duplicates` and
`app-leftovers`. `visual-studio` replaces `xcode`; Apple Mail, Homebrew and
simulator categories are omitted. `app-leftovers` covers only data folders
under `%LOCALAPPDATA%\Packages` whose valid Store/MSIX package family has
no registrations for the current user, as reported by the native
`GetPackagesByPackageFamily` API. Errors preserve the data, installed
families are excluded, and registration is rechecked before cleanup.
These items have `review` risk and are never preselected. Generic desktop
application data is kept because its ownership cannot safely be established.

Cache/log paths are an exact Windows allowlist exposed by
`scan.WindowsPaths`: browser caches, known app logs/crash dumps, user temp,
package download/build caches and Visual Studio component caches.
Application profiles, installed NuGet packages, settings and arbitrary
AppData folders are excluded. Native Known Folders supply Desktop,
Documents and Downloads, including redirected locations. All walkers skip
reparse points/junctions and avoid reading cloud-only file placeholders.

The Windows guard rejects device namespaces, alternate data streams,
wildcards, parent traversal, reserved device names and trailing-dot/space
aliases. Comparisons use Windows separators and case rules. It protects
volume/share roots, system locations, profile folders, Known Folder roots
and AppData containers. Only explicit cache/orphan-package targets and safe
selected dependencies inside the scanned project roots can extend the allowed area.

Ordinary `mode: "trash"` uses native `IFileOperation` with recycling/undo
flags and a progress sink that refuses permanent removal. Files that cannot
be recycled are failures; there is no permanent-delete fallback. The
Recycle Bin category is one fixed command item (`Windows Recycle Bin`),
queried read-only by `SHQueryRecycleBinW` for all drives. Emptying uses
`SHEmptyRecycleBinW`, requires `mode: "delete"`, and the panel requires a
separate permanent-deletion acknowledgement. It cannot accept raw paths.

Preferences/history live in `%LOCALAPPDATA%\Transom`. Setup installs into
`%LOCALAPPDATA%\Programs\Transom`, creates a Start Menu shortcut and registers
a current-user uninstall entry. Uninstall removes only owned program files,
leaving preferences/history and unrelated files intact.
