# Transom for Windows

## Download v0.2.3

| Your Windows PC | Setup installer | Portable ZIP |
| --- | --- | --- |
| x64 / Intel / AMD | [Download x64 Setup](https://github.com/anabiiil/transom/releases/download/v0.2.3/Transom-0.2.3-Setup-amd64.exe) | [Download x64 ZIP](https://github.com/anabiiil/transom/releases/download/v0.2.3/transom_0.2.3_windows_amd64.zip) |
| ARM64 | [Download ARM64 Setup](https://github.com/anabiiil/transom/releases/download/v0.2.3/Transom-0.2.3-Setup-arm64.exe) | [Download ARM64 ZIP](https://github.com/anabiiil/transom/releases/download/v0.2.3/transom_0.2.3_windows_arm64.zip) |

## التشغيل

- لمعظم أجهزة ويندوز استخدم نسخة **amd64 / x64**، ولأجهزة Windows on ARM استخدم **arm64**.
- للتثبيت شغّل `Transom-0.2.3-Setup-amd64.exe` أو `Transom-0.2.3-Setup-arm64.exe` حسب جهازك. التثبيت للمستخدم الحالي، ولا يحتاج صلاحيات مسؤول.
- للنسخة المحمولة فك ملف ZIP وشغّل `Transom.exe` مباشرة.
- افحص الملفات أولًا، راجع اختياراتك، ثم اضغط **Clean**. التنظيف الافتراضي يرسل الملفات إلى سلة المحذوفات. إفراغ السلة يحتاج اختيار **Delete permanently** وتأكيدًا منفصلًا.

## Requirements

Windows 10 or 11, x64 or ARM64, with the Microsoft Edge WebView2 Evergreen Runtime.
If Transom reports it missing, install it from
[Microsoft's WebView2 page](https://developer.microsoft.com/microsoft-edge/webview2/).
Go, Node, Python and .NET are not required to run the packaged application.
The WebView2 loader, original interface and icon are included in the EXE.
The WebView2 Runtime itself is installed separately if it is missing.

## Installation and portable use

The Setup EXE installs into `%LOCALAPPDATA%\Programs\Transom`, creates a
Start Menu shortcut and an entry in Windows' installed-apps list. Uninstall
through Windows Settings, or run `Uninstall.exe` in the installation folder.
Close Transom before installing an update or uninstalling.
The installer also includes the console companion, readme, license and
third-party notices. These builds are unsigned.

The ZIP contains `Transom.exe`, `transom-cli.exe`, this readme, the license and
third-party notices.
Double-click `Transom.exe`. Its desktop window opens without a console.
Preferences, cleanup history and the WebView2 profile are stored in
`%LOCALAPPDATA%\Transom`. Uninstall keeps these settings.

## Features and Windows adaptations

- The same Overview, System Junk, Developer, Projects, Files, History and
  Settings pages, original design and icon, light/dark/automatic themes,
  per-item selection and dry-run confirmation.
- A native desktop window without a console, with its size and position
  remembered between launches. Closing it shuts down the local panel server.
- Windows app/browser caches and logs, user temporary files and Recycle Bin.
- Windows package caches: npm, Yarn, pnpm, Composer, pip, Go,
  Gradle, NuGet and Bun; Visual Studio component caches; Docker cleanup.
- Stale `node_modules`, `vendor`, `.venv` and `target` folders under your
  configured project folders, large files, old downloads and duplicates.
- Reveal opens File Explorer and selects the scanned item.
- macOS-only Xcode, Homebrew, simulator and Apple Mail categories are replaced
  or omitted. App leftovers detects data folders of uninstalled Store/MSIX
  packages using current-user Windows registrations. Generic desktop-app
  data is kept because its ownership cannot be reliably established.

Scan only reads files. Cleaning accepts only item IDs from the current scan,
protects system/profile/AppData containers, and rejects junctions and other
reparse points. Ordinary cleanup uses Windows' native Recycle Bin, preserving
Restore information. If Windows cannot recycle a file, the item fails;
Transom does not silently delete it permanently.

Filesystem scanning excludes recognized applications, portable editors and
folders containing them. Cleanup checks this protection again before moving
or deleting an item, including items saved in an earlier scan. VS Code settings,
extensions, workspace state and unsaved-file backups are protected, including
custom `--user-data-dir` locations recognized by their settings or workspace
storage. If a folder cannot be fully inspected, it is kept. The standard
Windows editor profile's disposable `Cache`, `Code Cache`, `GPUCache` and `logs`
folders remain eligible for cleanup; its `User`, `Backups` and extensions do not.

Moving files to the Recycle Bin does not release disk space until the bin is
emptied. Cleanup sizes are estimates based on the scanned files.

## Command line

Use `transom-cli.exe` from PowerShell or Command Prompt:

```powershell
.\transom-cli.exe scan
.\transom-cli.exe scan --json
.\transom-cli.exe clean --dry-run --safe-only
.\transom-cli.exe ui
.\transom-cli.exe ui --browser
.\transom-cli.exe version
```

Project folders accept full Windows paths such as `C:\Users\you\Projects`,
`D:\Projects`, UNC shares, `~/Projects` and `~\Projects`.

## Build

From Windows with Go installed:

```powershell
powershell -ExecutionPolicy Bypass -File windows\build.ps1 -Version 0.2.3
```

From macOS or Linux with Go and `zip`:

```bash
VERSION=0.2.3 bash windows/build.sh
```

Both scripts build x64 and ARM64 executables and installers with the original
icon, version and DPI-aware manifest, and package ZIPs under `dist`.
The release workflow runs native Windows tests, checks the shared frontend,
and checks x64 setup, the installed CLI, uninstall and preservation of
unrelated installation-folder files. A local cross-build on macOS validates
compilation; it cannot verify the Windows desktop or Recycle Bin integration
at runtime. ARM64 builds are cross-compiled rather than executed by the x64
Windows release runner.
