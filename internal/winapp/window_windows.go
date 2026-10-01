//go:build windows

// Package winapp owns Transom's native Windows WebView2 window. The entire
// panel is supplied by the existing embedded UI; no second frontend is used.
package winapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

const runtimeURL = "https://developer.microsoft.com/microsoft-edge/webview2/"

// Run opens the same local panel used by the Mac app in a native Windows
// window. Closing that window returns to ui.RunDesktop, which stops its
// HTTP server and cancels any running scan.
func Run(ctx context.Context, panelURL string) error {
	if err := ctx.Err(); err != nil {
		return nil
	}
	version, err := webviewloader.GetInstalledVersion()
	if err != nil {
		return fmt.Errorf("checking Microsoft Edge WebView2 Runtime: %w\n\nInstall or repair the Evergreen WebView2 Runtime:\n%s\n\nYou can also open the panel with Transom.exe ui --browser", err, runtimeURL)
	}
	if version == "" {
		return fmt.Errorf("Microsoft Edge WebView2 Runtime is required to open Transom's desktop window.\n\nInstall the Evergreen Runtime from Microsoft, then open Transom.exe again:\n%s\n\nYou can also open the panel with Transom.exe ui --browser", runtimeURL)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("locating Transom's local application data folder: %w", err)
	}
	dataPath := filepath.Join(cacheDir, "Transom", "WebView2")
	if err := os.MkdirAll(dataPath, 0o700); err != nil {
		return fmt.Errorf("creating Transom's local application data folder: %w", err)
	}
	// WebView2 and its Win32 message queue belong to this one thread. The
	// binding initializes COM in its init function on the main Go thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// This binding reports a few fatal COM initialization errors through the
	// standard logger. Surface those failures before it exits a GUI process.
	previousLogOutput := log.Writer()
	log.SetOutput(io.MultiWriter(previousLogOutput, initializationErrors{}))
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		DataPath:  dataPath,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "Transom",
			Width:  1100,
			Height: 720,
			IconId: 1,
			Center: true,
		},
	})
	log.SetOutput(previousLogOutput)
	if w == nil {
		return errors.New("couldn't create Transom's desktop window. Install or repair Microsoft Edge WebView2 Runtime, or use Transom.exe ui --browser")
	}
	defer w.Destroy()
	w.SetSize(1100, 720, webview2.HintNone)
	w.SetSize(900, 600, webview2.HintMin)
	rememberWindow(w.Window(), filepath.Join(cacheDir, "Transom", "window.json"))
	// Opening an external link never gives that page the panel's local token.
	// The existing UI currently has no external links, but this also handles
	// future help links and keyboard window close consistently with the Mac.
	if err := w.Bind("transomOpenExternal", openExternal); err != nil {
		return fmt.Errorf("connecting Transom's desktop window: %w", err)
	}
	if err := w.Bind("transomCloseWindow", func() {
		w.Dispatch(func() { w.Destroy() })
	}); err != nil {
		return fmt.Errorf("connecting Transom's desktop window: %w", err)
	}
	parsed, err := url.Parse(panelURL)
	if err != nil {
		return fmt.Errorf("invalid panel address: %w", err)
	}
	origin, _ := json.Marshal(parsed.Scheme + "://" + parsed.Host)
	w.Init(`(() => {
const panelOrigin = ` + string(origin) + `;
const openURL = value => {
  let target;
  try { target = new URL(value, location.href); } catch (_) { return; }
  if (target.origin === panelOrigin) { location.href = target.href; return; }
  if (target.protocol === 'https:' || target.protocol === 'http:') {
    window.transomOpenExternal(target.href).catch(() => {});
  }
};
window.open = value => { if (value) openURL(value); return null; };
document.addEventListener('click', event => {
  const link = event.target.closest && event.target.closest('a[href]');
  if (!link) return;
  let target;
  try { target = new URL(link.href, location.href); } catch (_) { return; }
  if (target.origin !== panelOrigin || link.target === '_blank') {
    event.preventDefault(); openURL(target.href);
  }
});
document.addEventListener('keydown', event => {
  if (event.ctrlKey && event.key.toLowerCase() === 'w') {
    event.preventDefault(); window.transomCloseWindow();
  }
});
})();`)
	w.Navigate(panelURL)
	closed := make(chan struct{})
	defer close(closed)
	go func() {
		select {
		case <-ctx.Done():
			w.Dispatch(func() { w.Destroy() })
		case <-closed:
		}
	}()
	w.Run()
	return nil
}

func openExternal(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("only HTTP and HTTPS links can be opened")
	}
	verb, _ := windows.UTF16PtrFromString("open")
	path, err := windows.UTF16PtrFromString(u.String())
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, path, nil, nil, windows.SW_SHOWNORMAL)
}

// ShowError is used by Windows GUI builds where stderr has no visible owner.
func ShowError(err error) {
	message, _ := windows.UTF16PtrFromString("Transom couldn't start.\n\n" + err.Error())
	title, _ := windows.UTF16PtrFromString("Transom")
	_, _ = windows.MessageBox(0, message, title, windows.MB_OK|windows.MB_ICONERROR)
}

type initializationErrors struct{}

func (initializationErrors) Write(message []byte) (int, error) {
	ShowError(fmt.Errorf("Microsoft Edge WebView2 could not initialize:\n%s\nInstall or repair the Evergreen Runtime:\n%s\n\nYou can also use Transom.exe ui --browser", message, runtimeURL))
	return len(message), nil
}
