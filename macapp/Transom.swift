// Transom.swift — a tiny AppKit shell that owns a real window around the
// transom control panel. Compiled directly with swiftc (no Xcode project,
// no storyboard): see build.sh for how this becomes Transom.app.
//
// The panel itself is served by the transom CLI (`transom ui --window-host`);
// this app just launches that child process, waits for it to print the
// panel's URL, and shows it in a WKWebView. See macapp/build.sh and the Go
// side (internal/macapp) for the other half of the contract, and
// docs/CONTRACT.md for the full handshake this depends on.
//
// Adapted from Mullion's macapp/Mullion.swift (../mullion). Transom is a
// single-window utility (one control panel, no terminal/project popout
// windows and no native file-picker bridge — Transom never needs a real
// filesystem path out of the browser), so that machinery was dropped;
// everything else (child process handshake, same-origin navigation
// handling, external link handoff, native tabs identifier, clean
// shutdown) is carried over near verbatim.

import Cocoa
import WebKit

// MARK: - Locating the transom binary
//
// Apps launched from Finder don't inherit the user's shell PATH, so we
// can't just exec "transom" and hope the shell finds it. Prefer the copy
// bundled inside the app (build.sh copies a freshly built Go binary to
// Contents/Resources/transom so the app is self-contained), then fall
// back to the known install locations a standalone `transom` binary
// would use.
func locateTransomBinary() -> String? {
    var candidates: [String] = []
    if let resourcePath = Bundle.main.resourcePath {
        candidates.append(resourcePath + "/transom")
    }
    candidates.append(("~/.transom/bin/transom" as NSString).expandingTildeInPath)
    candidates.append("/opt/homebrew/bin/transom")
    candidates.append("/usr/local/bin/transom")

    for path in candidates where FileManager.default.isExecutableFile(atPath: path) {
        return path
    }
    return nil
}

private let transomURLPrefix = "TRANSOM_UI_URL="

// A NSWindow.tabbingIdentifier for the main window. Transom only ever has
// one window, but setting this costs nothing and keeps the door open for
// AppKit's native "Merge All Windows" / tab bar if a future window is
// ever added.
private let windowTabbingIdentifier = "transom"

// Returns whether two URLs share a host+port — the same test the
// navigation delegate uses to decide "stay in the app" vs. "hand off to
// the default browser". Pulled out as a pure function so it's easy to
// exercise on its own without spinning up any WebKit/AppKit objects.
func isSameOrigin(_ a: URL, _ b: URL) -> Bool {
    let portA = a.port ?? (a.scheme == "https" ? 443 : 80)
    let portB = b.port ?? (b.scheme == "https" ? 443 : 80)
    return a.host == b.host && portA == portB
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    var window: NSWindow!
    var webView: WKWebView!
    var loadingView: NSView!
    var loadingLabel: NSTextField!
    var loadingSpinner: NSProgressIndicator!

    var childProcess: Process?
    var childStdin: Pipe?
    var panelURL: URL?
    var stderrTail: [String] = []
    var urlResolved = false

    // MARK: Lifecycle

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.regular)
        buildMainMenu()
        buildWindow()
        NSApp.activate(ignoringOtherApps: true)
        startChildProcess()
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        true
    }

    // Clicking the Dock icon while the panel is already running should
    // just bring the window forward, not spawn anything new.
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        window.makeKeyAndOrderFront(nil)
        return true
    }

    func applicationWillTerminate(_ notification: Notification) {
        stopChildProcess()
    }

    // MARK: Menu
    //
    // Built by hand since there's no storyboard/xib to supply the default
    // menu bar. The Edit menu's Undo/Redo/Cut/Copy/Paste/Select All use the
    // standard first-responder selectors (no explicit target) so they reach
    // whatever WKWebView's internal text field currently has focus — that's
    // what makes ⌘C/⌘V work while typing into the panel.
    func buildMainMenu() {
        let mainMenu = NSMenu()

        let appMenuItem = NSMenuItem()
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "About Transom", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        appMenu.addItem(NSMenuItem.separator())
        appMenu.addItem(withTitle: "Hide Transom", action: #selector(NSApplication.hide(_:)), keyEquivalent: "h")
        appMenu.addItem(NSMenuItem.separator())
        appMenu.addItem(withTitle: "Quit Transom", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appMenuItem.submenu = appMenu
        mainMenu.addItem(appMenuItem)

        let editMenuItem = NSMenuItem()
        let editMenu = NSMenu(title: "Edit")
        editMenu.addItem(withTitle: "Undo", action: Selector(("undo:")), keyEquivalent: "z")
        editMenu.addItem(withTitle: "Redo", action: Selector(("redo:")), keyEquivalent: "Z")
        editMenu.addItem(NSMenuItem.separator())
        editMenu.addItem(withTitle: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        editMenu.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(NSMenuItem.separator())
        editMenu.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        editMenuItem.submenu = editMenu
        mainMenu.addItem(editMenuItem)

        let viewMenuItem = NSMenuItem()
        let viewMenu = NSMenu(title: "View")
        viewMenu.addItem(withTitle: "Reload", action: #selector(reload(_:)), keyEquivalent: "r")
        viewMenuItem.submenu = viewMenu
        mainMenu.addItem(viewMenuItem)

        let windowMenuItem = NSMenuItem()
        let windowMenu = NSMenu(title: "Window")
        windowMenu.addItem(withTitle: "Minimize", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        windowMenu.addItem(withTitle: "Close", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        windowMenuItem.submenu = windowMenu
        mainMenu.addItem(windowMenuItem)
        // Lets AppKit append the standard "Bring All to Front" etc. items.
        NSApp.windowsMenu = windowMenu

        NSApp.mainMenu = mainMenu
    }

    @objc func reload(_ sender: Any?) {
        webView?.reload()
    }

    // MARK: Window

    func buildWindow() {
        let rect = NSRect(x: 0, y: 0, width: 1100, height: 720)
        window = NSWindow(
            contentRect: rect,
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered,
            defer: false
        )
        window.title = "Transom"
        window.minSize = NSSize(width: 900, height: 600)
        window.center()
        // Remembers the user's size/position across launches; centering
        // above only applies the very first time (no saved frame yet).
        window.setFrameAutosaveName("TransomMain")
        window.tabbingIdentifier = windowTabbingIdentifier
        window.tabbingMode = .automatic

        window.contentView = buildLoadingView(frame: rect)
        window.makeKeyAndOrderFront(nil)
    }

    // A lightweight native "starting up" view, shown in place of the
    // control panel until the child process is up AND the page has
    // actually finished loading (not just until we have a URL to point
    // the web view at).
    func buildLoadingView(frame: NSRect) -> NSView {
        let container = NSView(frame: frame)
        container.autoresizingMask = [.width, .height]

        let spinner = NSProgressIndicator()
        spinner.style = .spinning
        spinner.controlSize = .regular
        spinner.isIndeterminate = true
        spinner.sizeToFit()
        spinner.startAnimation(nil)

        let label = NSTextField(labelWithString: "Starting Transom…")
        label.font = NSFont.systemFont(ofSize: 13)
        label.textColor = .secondaryLabelColor
        label.alignment = .center
        label.sizeToFit()

        let spinnerSize = spinner.frame.size
        let labelSize = label.frame.size
        let spacing: CGFloat = 12
        let totalHeight = spinnerSize.height + spacing + labelSize.height
        let midX = frame.width / 2
        let midY = frame.height / 2

        spinner.frame = NSRect(
            x: midX - spinnerSize.width / 2,
            y: midY - totalHeight / 2 + labelSize.height + spacing,
            width: spinnerSize.width, height: spinnerSize.height
        )
        label.frame = NSRect(
            x: midX - labelSize.width / 2,
            y: midY - totalHeight / 2,
            width: labelSize.width, height: labelSize.height
        )
        spinner.autoresizingMask = [.minXMargin, .maxXMargin, .minYMargin, .maxYMargin]
        label.autoresizingMask = [.minXMargin, .maxXMargin, .minYMargin, .maxYMargin]

        container.addSubview(spinner)
        container.addSubview(label)
        loadingSpinner = spinner
        loadingLabel = label
        return container
    }

    // Builds the web view (hidden behind the loading view, which stays on
    // top until the page finishes loading) and starts navigation to the
    // resolved panel URL.
    func showWebView() {
        let configuration = WKWebViewConfiguration()
        let webView = WKWebView(frame: window.contentView!.bounds, configuration: configuration)
        webView.autoresizingMask = [.width, .height]
        webView.navigationDelegate = self
        webView.uiDelegate = self
        self.webView = webView

        // Keep the loading view around as an overlay so it stays visible
        // until didFinish fires, instead of swapping contentView outright
        // (which would show a blank white flash before the page paints).
        let loading = window.contentView!
        let host = NSView(frame: window.contentView!.bounds)
        host.autoresizingMask = [.width, .height]
        host.addSubview(webView)
        loading.frame = host.bounds
        loading.autoresizingMask = [.width, .height]
        host.addSubview(loading)
        window.contentView = host
        self.loadingView = loading

        if let url = panelURL {
            webView.load(URLRequest(url: url))
        }
    }

    func hideLoadingView() {
        guard let loading = loadingView else { return }
        loadingSpinner?.stopAnimation(nil)
        loading.removeFromSuperview()
        loadingView = nil
    }

    // MARK: Child process
    //
    // Contract with the Go side (internal/macapp) and docs/CONTRACT.md: run
    // `<transom> ui --window-host`, read stdout for a single
    // "TRANSOM_UI_URL=..." line once the server is listening, then keep the
    // child's stdin pipe open until we quit (closing it is the child's
    // signal to stop serving).

    func startChildProcess() {
        guard let binaryPath = locateTransomBinary() else {
            fail(reason: """
            Could not find the transom binary. Checked:
              Contents/Resources/transom (bundled)
              ~/.transom/bin/transom
              /opt/homebrew/bin/transom
              /usr/local/bin/transom
            """)
            return
        }

        let process = Process()
        process.executableURL = URL(fileURLWithPath: binaryPath)
        process.arguments = ["ui", "--window-host"]

        let stdinPipe = Pipe()
        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        process.standardInput = stdinPipe
        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe
        childStdin = stdinPipe
        childProcess = process

        var stdoutBuffer = Data()
        stdoutPipe.fileHandleForReading.readabilityHandler = { [weak self] handle in
            let data = handle.availableData
            guard !data.isEmpty else { return }
            stdoutBuffer.append(data)
            self?.scanForURL(in: &stdoutBuffer)
        }

        stderrPipe.fileHandleForReading.readabilityHandler = { [weak self] handle in
            let data = handle.availableData
            guard !data.isEmpty, let text = String(data: data, encoding: .utf8) else { return }
            DispatchQueue.main.async {
                self?.appendStderr(text)
            }
        }

        process.terminationHandler = { [weak self] proc in
            DispatchQueue.main.async {
                guard let self = self, !self.urlResolved else { return }
                self.fail(reason: "transom exited before starting the control panel (status \(proc.terminationStatus)).")
            }
        }

        do {
            try process.run()
        } catch {
            fail(reason: "Failed to launch transom: \(error.localizedDescription)")
            return
        }

        DispatchQueue.main.asyncAfter(deadline: .now() + 20) { [weak self] in
            guard let self = self, !self.urlResolved else { return }
            self.fail(reason: "Timed out waiting for the control panel to start.")
        }
    }

    // Runs on the readabilityHandler's background queue; only the URL
    // hand-off dispatches back to main.
    func scanForURL(in buffer: inout Data) {
        while let newline = buffer.firstIndex(of: 0x0A) {
            let lineData = buffer.subdata(in: buffer.startIndex..<newline)
            buffer.removeSubrange(buffer.startIndex...newline)
            guard let line = String(data: lineData, encoding: .utf8) else { continue }
            let trimmed = line.trimmingCharacters(in: .whitespacesAndNewlines)
            guard trimmed.hasPrefix(transomURLPrefix) else { continue }
            guard let url = URL(string: String(trimmed.dropFirst(transomURLPrefix.count))) else { continue }
            DispatchQueue.main.async { [weak self] in
                self?.handleResolvedURL(url)
            }
        }
    }

    func handleResolvedURL(_ url: URL) {
        guard !urlResolved else { return }
        urlResolved = true
        panelURL = url
        showWebView()
    }

    func appendStderr(_ text: String) {
        stderrTail.append(text)
        // Keep only a reasonable tail so a runaway child can't balloon the
        // eventual error alert.
        let joined = stderrTail.joined()
        if joined.utf8.count > 4000 {
            stderrTail = [String(joined.suffix(4000))]
        }
    }

    func fail(reason: String) {
        guard !urlResolved else { return } // a working panel outlives stray stderr/exit noise
        stopChildProcess()
        let alert = NSAlert()
        alert.alertStyle = .critical
        alert.messageText = "Transom couldn't start"
        var text = reason
        if !stderrTail.isEmpty {
            text += "\n\n" + stderrTail.joined()
        }
        alert.informativeText = text
        alert.addButton(withTitle: "Quit")
        alert.runModal()
        NSApp.terminate(nil)
    }

    // Closes the child's stdin (its signal to stop serving), sends
    // SIGTERM, then waits briefly for it to actually exit before falling
    // back to SIGKILL — so we don't leave an orphaned server process
    // behind when the app quits.
    func stopChildProcess() {
        childStdin?.fileHandleForWriting.closeFile()
        childStdin = nil
        guard let process = childProcess else { return }
        process.terminationHandler = nil
        if process.isRunning {
            process.terminate() // SIGTERM
            let deadline = Date().addingTimeInterval(3)
            while process.isRunning && Date() < deadline {
                Thread.sleep(forTimeInterval: 0.05)
            }
            if process.isRunning {
                kill(process.processIdentifier, SIGKILL)
            }
        }
        childProcess = nil
    }
}

// MARK: - WKNavigationDelegate / WKUIDelegate

extension AppDelegate: WKNavigationDelegate, WKUIDelegate {
    // Keep the panel's own host+port inside the app window; send everything
    // else (external http/https links) to the user's default browser
    // instead.
    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction, decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let url = navigationAction.request.url, let panel = panelURL else {
            decisionHandler(.allow)
            return
        }
        if isSameOrigin(url, panel) {
            decisionHandler(.allow)
        } else {
            decisionHandler(.cancel)
            NSWorkspace.shared.open(url)
        }
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        hideLoadingView()
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        hideLoadingView()
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        hideLoadingView()
    }

    // window.open / target=_blank. Transom is a single-window app with no
    // popout windows, so same-origin requests just load in the main
    // webview and anything external goes to the default browser — either
    // way we never hand back a new WKWebView.
    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration, for navigationAction: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        guard let url = navigationAction.request.url else { return nil }
        if let panel = panelURL, isSameOrigin(url, panel) {
            webView.load(URLRequest(url: url))
        } else {
            NSWorkspace.shared.open(url)
        }
        return nil
    }

    func webView(_ webView: WKWebView, runJavaScriptAlertPanelWithMessage message: String, initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping () -> Void) {
        let alert = NSAlert()
        alert.messageText = message
        alert.addButton(withTitle: "OK")
        alert.beginSheetModal(for: window) { _ in completionHandler() }
    }

    func webView(_ webView: WKWebView, runJavaScriptConfirmPanelWithMessage message: String, initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping (Bool) -> Void) {
        let alert = NSAlert()
        alert.messageText = message
        alert.addButton(withTitle: "OK")
        alert.addButton(withTitle: "Cancel")
        alert.beginSheetModal(for: window) { response in
            completionHandler(response == .alertFirstButtonReturn)
        }
    }

    func webView(_ webView: WKWebView, runJavaScriptTextInputPanelWithPrompt prompt: String, defaultText: String?, initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping (String?) -> Void) {
        let alert = NSAlert()
        alert.messageText = prompt
        alert.addButton(withTitle: "OK")
        alert.addButton(withTitle: "Cancel")
        let field = NSTextField(frame: NSRect(x: 0, y: 0, width: 260, height: 24))
        field.stringValue = defaultText ?? ""
        alert.accessoryView = field
        alert.window.initialFirstResponder = field
        alert.beginSheetModal(for: window) { response in
            completionHandler(response == .alertFirstButtonReturn ? field.stringValue : nil)
        }
    }
}

// MARK: - Entry point

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
