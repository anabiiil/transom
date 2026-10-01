package scan

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// App leftovers are deliberately conservative — a false positive means
// deleting a live app's data, a miss only means some megabytes stay.
// An entry is reported only when:
//   - its name is a reverse-DNS bundle id starting with a real TLD
//     (plain names like "Google" or "Code" are never guessed at),
//   - it isn't Apple's, nor a well-known SDK's shared by many apps,
//   - no installed app, login item or launch agent shares its vendor
//     (first two components): with any com.microsoft.* app installed,
//     nothing com.microsoft.* is reported,
//   - none of its components names an installed app or vendor,
//   - and the installed-app inventory completed without unreadable apps
//     or skipped directories. Missing information is not an uninstall.

var bundleIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*\.[A-Za-z0-9-]+(\.[A-Za-z0-9_-]+)+$`)

// sharedVendors are SDKs/frameworks that write under their own id on
// behalf of whichever app embeds them.
var sharedVendors = map[string]bool{
	"com.apple": true, "group.com": true, "systemgroup.com": true,
	"com.crashlytics": true, "io.fabric": true, "com.plausiblelabs": true,
	"io.sentry": true, "com.segment": true, "com.firebase": true, "com.google": true,
	"com.mixpanel": true, "com.bugsnag": true, "net.hockeyapp": true, "com.hockeyapp": true,
	"org.sparkle-project": true, "com.microsoft": true, "com.oracle": true,
	"org.python": true, "org.nodejs": true, "org.mozilla": true, "com.github": true,
	"io.github": true, "org.chromium": true, "com.electron": true, "com.squirrel": true,
}

// leftoverDirs are where apps leave data, with the suffix their entries
// carry after the bundle id.
var leftoverDirs = []struct{ sub, suffix, label string }{
	{"Application Support", "", "Application Support"},
	{"Caches", "", "Caches"},
	{"Containers", "", "Container"},
	{"Preferences", ".plist", "Preferences"},
	{"Saved Application State", ".savedState", "Saved state"},
}

// teamPrefixRe matches the Team ID some sandboxed helpers prefix their
// bundle id with ("S8EX82NJP6.com.vendor.app").
var teamPrefixRe = regexp.MustCompile(`^[A-Z0-9]{10}\.`)

// stripTeam drops a leading Team ID.
func stripTeam(id string) string { return teamPrefixRe.ReplaceAllString(id, "") }

// reverseDNSRoots are the first components a real bundle id starts with.
// Anything else ("jetbrains.ps.262") is some tool's private naming and
// is left alone.
var reverseDNSRoots = map[string]bool{
	"com": true, "org": true, "net": true, "io": true, "co": true, "de": true,
	"app": true, "dev": true, "me": true, "uk": true, "fr": true, "nl": true,
	"ch": true, "at": true, "se": true, "jp": true, "ru": true, "cn": true,
	"eu": true, "us": true, "ca": true, "info": true, "tv": true, "ai": true,
}

func vendorOf(id string) string {
	parts := strings.SplitN(strings.ToLower(stripTeam(id)), ".", 3)
	if len(parts) < 2 {
		return strings.ToLower(id)
	}
	return parts[0] + "." + parts[1]
}

// installedApps is what counts as "still installed".
type installedApps struct {
	vendors             map[string]bool // lowercased "com.vendor"
	vendorNames         map[string]bool // lowercased "vendor" part of those
	names               map[string]bool // lowercased app names, spaces removed
	inventoryIncomplete bool
}

func (ia *installedApps) addID(id string) {
	v := vendorOf(id)
	ia.vendors[v] = true
	if i := strings.IndexByte(v, '.'); i >= 0 && len(v)-i-1 >= 4 {
		ia.vendorNames[v[i+1:]] = true
	}
}

func normName(s string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "-", "", "_", "").Replace(s))
}

// isLeftover applies the rules above to a bundle-id-like entry name.
func (ia *installedApps) isLeftover(id string) bool {
	if ia.inventoryIncomplete {
		return false
	}
	id = stripTeam(id)
	if !bundleIDRe.MatchString(id) {
		return false
	}
	low := strings.ToLower(id)
	if !reverseDNSRoots[low[:strings.IndexByte(low, '.')]] {
		return false
	}
	if strings.HasPrefix(low, "com.apple") || strings.HasPrefix(low, "group.") || strings.HasPrefix(low, "systemgroup.") {
		return false
	}
	v := vendorOf(id)
	if sharedVendors[v] || ia.vendors[v] {
		return false
	}
	for _, part := range strings.Split(low, ".")[1:] {
		if len(part) >= 3 && ia.names[normName(part)] {
			return false
		}
		if ia.vendorNames[part] { // com.jetbrains.* installed, entry io.jetbrains.x
			return false
		}
	}
	return true
}

func scanAppLeftovers(ctx context.Context, e *Env) ([]Item, error) {
	if runtime.GOOS == "windows" {
		items := windowsItems(ctx, e, "app-leftovers")
		for i := range items {
			items[i].Label = filepath.Base(items[i].Path) + " (Store/MSIX app data)"
			items[i].Note = "No package in this family is registered for your Windows account; review saved app data before cleaning"
		}
		return items, ctx.Err()
	}
	return scanAppLeftoversWithInstalled(ctx, e, collectInstalled(ctx, e.Home))
}

func scanAppLeftoversWithInstalled(ctx context.Context, e *Env, ia *installedApps) ([]Item, error) {
	if ia.inventoryIncomplete || len(ia.vendors) == 0 {
		// A partial inventory cannot prove an app was uninstalled.
		return nil, nil
	}
	lib := filepath.Join(e.Home, "Library")
	type cand struct{ label, id string }
	info := map[string]cand{}
	var paths []string
	for _, d := range leftoverDirs {
		for _, p := range children(filepath.Join(lib, d.sub)) {
			name := filepath.Base(p)
			if d.suffix != "" {
				if !strings.HasSuffix(name, d.suffix) {
					continue
				}
				name = strings.TrimSuffix(name, d.suffix)
			}
			if !ia.isLeftover(name) {
				continue
			}
			info[p] = cand{label: name + " (" + d.label + ")", id: name}
			paths = append(paths, p)
		}
	}
	var items []Item
	for _, s := range e.sizePaths(ctx, paths) {
		c := info[s.path]
		items = append(items, Item{
			Path:    s.path,
			Label:   c.label,
			Size:    s.size,
			ModTime: rfc3339(s.newest),
			Kind:    kindOf(s.info),
			Note:    "No installed app from " + vendorOf(c.id) + " found",
		})
	}
	return items, nil
}

// ValidateAppLeftover rechecks ownership immediately before cleanup. A
// reinstall since the scan, or an incomplete current inventory, preserves
// the app's saved data. The caller must still apply its normal path guard.
func ValidateAppLeftover(ctx context.Context, home, path string) error {
	ia := collectInstalled(ctx, home)
	if err := ctx.Err(); err != nil {
		return err
	}
	return validateAppLeftoverWithInstalled(home, path, ia)
}

func validateAppLeftoverWithInstalled(home, path string, ia *installedApps) error {
	for _, d := range leftoverDirs {
		if filepath.Dir(path) != filepath.Join(home, "Library", d.sub) {
			continue
		}
		name := filepath.Base(path)
		if d.suffix != "" {
			if !strings.HasSuffix(name, d.suffix) {
				continue
			}
			name = strings.TrimSuffix(name, d.suffix)
		}
		if len(ia.vendors) > 0 && ia.isLeftover(name) {
			return nil
		}
		return fmt.Errorf("preserving application data: uninstall could not be confirmed")
	}
	return fmt.Errorf("not an application-leftover entry")
}

// collectInstalled finds installed apps (the standard folders plus
// whatever Spotlight knows about, e.g. apps on other volumes) and login
// items / launch agents, and records their vendors and names.
func collectInstalled(ctx context.Context, home string) *installedApps {
	ia := &installedApps{vendors: map[string]bool{}, vendorNames: map[string]bool{}, names: map[string]bool{}}
	apps := map[string]bool{}
	for _, root := range []string{"/Applications", filepath.Join(home, "Applications"), "/System/Applications", "/Library/PreferencePanes", filepath.Join(home, "Library", "PreferencePanes")} {
		if !findApps(root, 0, apps) {
			ia.inventoryIncomplete = true
		}
	}
	if mdfind := FindTool("mdfind"); mdfind != "" {
		if out, err := output(ctx, 15*time.Second, mdfind, "-0", "kMDItemContentType == 'com.apple.application-bundle'"); err == nil {
			for _, p := range bytes.Split(out, []byte{0}) {
				if len(p) > 0 {
					apps[string(p)] = true
				}
			}
		} else {
			ia.inventoryIncomplete = true
		}
	} else {
		ia.inventoryIncomplete = true
	}

	var list []string
	for p := range apps {
		list = append(list, p)
		ia.names[normName(strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)))] = true
	}
	ids, complete := bundleIDs(ctx, list)
	if !complete {
		ia.inventoryIncomplete = true
	}
	for _, id := range ids {
		ia.addID(id)
	}
	// Background helpers without an app of their own.
	for _, dir := range []string{"/Library/LaunchAgents", "/Library/LaunchDaemons", filepath.Join(home, "Library", "LaunchAgents")} {
		entries, err := readDirUnsorted(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				ia.inventoryIncomplete = true
			}
			continue
		}
		for _, entry := range entries {
			if id := strings.TrimSuffix(entry.Name(), ".plist"); bundleIDRe.MatchString(id) {
				ia.addID(id)
			}
		}
	}
	return ia
}

// findApps collects .app bundles (and prefpanes) up to 3 levels deep
// (e.g. /Applications/Utilities/X.app, /Applications/Setapp/X.app).
// False means a directory was unreadable or beyond that bound, so apps
// absent from the result must not be presumed uninstalled.
func findApps(dir string, depth int, into map[string]bool) bool {
	if depth > 3 {
		return false
	}
	entries, err := readDirUnsorted(dir)
	if err != nil {
		// Some platforms report a directory-read error on an existing
		// regular file as "not found". Only a genuinely absent optional
		// root is harmless; an existing or unverifiable root is incomplete.
		if os.IsNotExist(err) {
			_, statErr := os.Lstat(dir)
			return os.IsNotExist(statErr)
		}
		return false
	}
	complete := true
	for _, entry := range entries {
		p := filepath.Join(dir, entry.Name())
		fi, err := os.Lstat(p)
		if err != nil {
			complete = false
			continue
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext == ".app" || ext == ".prefpane" {
			into[p] = true
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			// Do not enumerate linked custom installation directories, but
			// do not mistake the apps they may contain for uninstalled ones.
			if target, err := os.Stat(p); err != nil || target.IsDir() {
				complete = false
			}
			continue
		}
		if fi.IsDir() && !isBundle(p) {
			if !findApps(p, depth+1, into) {
				complete = false
			}
		}
	}
	return complete
}

// infoPlists returns candidate Info.plist paths of a bundle, covering
// iOS apps installed on Apple silicon (Wrapper/X.app/Info.plist).
func infoPlists(app string) []string {
	out := []string{filepath.Join(app, "Contents", "Info.plist")}
	for _, inner := range children(filepath.Join(app, "Wrapper")) {
		if strings.HasSuffix(inner, ".app") {
			out = append(out, filepath.Join(inner, "Info.plist"))
		}
	}
	return out
}

// bundleIDs reads CFBundleIdentifier of every bundle via plutil
// (Info.plist is often binary), in parallel.
func bundleIDs(ctx context.Context, apps []string) ([]string, bool) {
	plutil := FindTool("plutil")
	if plutil == "" {
		return nil, false
	}
	return bundleIDsWithReader(ctx, apps, func(pl string) (string, error) {
		out, err := output(ctx, 5*time.Second, plutil, "-extract", "CFBundleIdentifier", "raw", "-o", "-", "--", pl)
		return strings.TrimSpace(string(out)), err
	})
}

func bundleIDsWithReader(ctx context.Context, apps []string, readID func(string) (string, error)) ([]string, bool) {
	var (
		mu       sync.Mutex
		ids      []string
		wg       sync.WaitGroup
		complete = true
	)
	work := make(chan string)
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for app := range work {
				if ctx.Err() != nil {
					mu.Lock()
					complete = false
					mu.Unlock()
					continue
				}
				foundID := false
				for _, pl := range infoPlists(app) {
					if _, err := os.Stat(pl); err != nil {
						continue
					}
					id, err := readID(pl)
					if err == nil && bundleIDRe.MatchString(stripTeam(id)) {
						mu.Lock()
						ids = append(ids, id)
						mu.Unlock()
						foundID = true
					}
				}
				if !foundID {
					mu.Lock()
					complete = false
					mu.Unlock()
				}
			}
		}()
	}
	for _, a := range apps {
		work <- a
	}
	close(work)
	wg.Wait()
	return ids, complete
}
