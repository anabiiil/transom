# Transom — internal contract

Transom is a macOS disk cleaner, a sibling of Mullion (`../mullion`): one Go binary
(cobra CLI, module `transom`), an embedded web control panel served on 127.0.0.1,
and a native `Transom.app` (AppKit + WKWebView) that hosts the panel.

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
