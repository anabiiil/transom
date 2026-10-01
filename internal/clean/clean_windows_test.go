//go:build windows

package clean

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"transom/internal/scan"
)

func windowsFixture(t *testing.T) (home, temp, outside string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, temp, outside = filepath.Join(base, "home"), filepath.Join(base, "temp"), filepath.Join(base, "outside")
	for _, dir := range []string{home, temp, outside, filepath.Join(home, "AppData", "Local"), filepath.Join(home, "AppData", "Roaming")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range map[string]string{"USERPROFILE": home, "HOME": home, "APPDATA": filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA": filepath.Join(home, "AppData", "Local"), "TMP": temp, "TEMP": temp} {
		t.Setenv(key, value)
	}
	return home, temp, outside
}

func windowsWrite(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("test data"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsPathSyntaxAndContainment(t *testing.T) {
	for _, p := range []string{`C:\Users\Ana\cache`, `c:/Users/Ana/cache`, `\\server\share\Projects\app\node_modules`} {
		if err := validateWindowsPath(p); err != nil {
			t.Errorf("valid %q: %v", p, err)
		}
	}
	for _, p := range []string{"", `relative\cache`, `C:cache`, `\cache`, `C:\Users\Ana\..\cache`, `C:/Users/Ana/../cache`, `C:\Users\Ana\file:stream`, `C:\Users\Ana\folder.`, `C:\Users\Ana\folder `, `C:\Users\Ana\CON.txt`, `C:\Users\Ana\LPT1`, `C:\Users\Ana\COM¹`, `C:\Users\Ana\LPT².txt`, `C:\Users\Ana\*.log`, `\\?\C:\Users\Ana\cache`, `\\.\C:\Users\Ana\cache`, `\\server\share:stream\cache`, "C:\\Users\\Ana\\\x01name"} {
		if err := validateWindowsPath(p); err == nil {
			t.Errorf("unsafe path accepted: %q", p)
		}
	}
	for _, test := range []struct {
		path, root string
		want       bool
	}{
		{`C:\USERS\ANA\cache`, `c:\Users\Ana`, true},
		{`C:/Users/Ana/cache`, `c:\Users\Ana`, true},
		{`C:\Users\Ana`, `c:\Users\Ana`, false},
		{`C:\Users\Anabel\cache`, `C:\Users\Ana`, false},
		{`D:\Users\Ana\cache`, `C:\Users\Ana`, false},
		{`\\server\share\cache`, `\\SERVER\SHARE`, true},
		{`\\server\share2\cache`, `\\server\share`, false},
	} {
		if got := within(test.path, test.root); got != test.want {
			t.Errorf("within(%q, %q) = %v", test.path, test.root, got)
		}
	}
}

func TestWindowsProjectRootProtection(t *testing.T) {
	windowsFixture(t)
	for _, root := range []string{`C:\`, `D:\`, `\\server\share`, `C:\Windows\Temp`, `D:\Windows\code`, `C:\Program Files\Code`, `C:\ProgramData\code`, `C:\Users`, `C:\Users\Ana`, `C:\Users\Ana\AppData\Local\code`, `D:\$Recycle.Bin\code`, `D:\System Volume Information\code`} {
		if unsafeProjectRoot(root) == nil {
			t.Errorf("unsafe project root accepted: %q", root)
		}
	}
	for _, root := range []string{`D:\Projects`, `C:\Users\Ana\Work`, `\\server\share\Projects`} {
		if err := unsafeProjectRoot(root); err != nil {
			t.Errorf("safe project root %q: %v", root, err)
		}
	}
}

func TestWindowsGuardAllowsOnlyApprovedAppDataCache(t *testing.T) {
	home, temp, outside := windowsFixture(t)
	cache := filepath.Join(home, "AppData", "Local", "Google", "Chrome", "User Data", "Default", "Cache")
	windowsWrite(t, filepath.Join(cache, "data.bin"))
	state := filepath.Join(home, "AppData", "Local", "Google", "Chrome", "User Data", "Default", "Cookies")
	windowsWrite(t, state)
	windowsWrite(t, filepath.Join(home, "Documents", "large.bin"))
	windowsWrite(t, filepath.Join(outside, "keep.bin"))
	windowsWrite(t, filepath.Join(temp, "stale.bin"))
	g := platformGuard(Guard{Home: home, TempRoots: []string{temp}}, scan.Item{Category: "user-caches"})
	for _, p := range []string{cache, filepath.Join(cache, "data.bin"), filepath.Join(home, "Documents", "large.bin"), filepath.Join(temp, "stale.bin")} {
		if _, err := g.Check(p); err != nil {
			t.Errorf("approved %q: %v", p, err)
		}
	}
	for _, p := range []string{home, filepath.Dir(home), filepath.Join(home, "Documents"), filepath.Join(home, "AppData"), filepath.Join(home, "AppData", "Local"), filepath.Join(home, "AppData", "Roaming"), filepath.Dir(cache), state, temp, filepath.Join(outside, "keep.bin")} {
		if got, err := g.Check(p); err == nil {
			t.Errorf("protected %q accepted as %q", p, got)
		}
	}
	if _, err := (Guard{Home: home}).Check(cache); err == nil {
		t.Error("AppData cache allowed without approved roots")
	}
	// A category label alone cannot grant access to application state.
	stateGuard := platformGuard(Guard{Home: home}, scan.Item{Category: "user-caches"})
	if _, err := stateGuard.Check(state); err == nil {
		t.Error("category label allowed browser Cookies")
	}
}

func TestWindowsGuardRedirectedAppData(t *testing.T) {
	home, _, outside := windowsFixture(t)
	local := filepath.Join(outside, "Local")
	roaming := filepath.Join(outside, "Roaming")
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("APPDATA", roaming)
	cache := filepath.Join(roaming, "Code", "Cache")
	windowsWrite(t, filepath.Join(cache, "data.bin"))
	windowsWrite(t, filepath.Join(roaming, "Code", "User", "settings.json"))
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	g := platformGuard(Guard{Home: home}, scan.Item{Category: "user-caches"})
	if _, err := g.Check(cache); err != nil {
		t.Fatalf("redirected cache refused: %v", err)
	}
	for _, p := range []string{roaming, local, filepath.Join(roaming, "Code"), filepath.Join(roaming, "Code", "User", "settings.json")} {
		if _, err := g.Check(p); err == nil {
			t.Errorf("redirected state/container accepted: %q", p)
		}
	}
}

// Junctions require no symbolic-link privilege on NTFS. Build one through
// the native reparse API so path names never enter command text.
func windowsJunction(t *testing.T, link, target string) {
	t.Helper()
	if err := os.MkdirAll(link, 0o755); err != nil {
		t.Fatal(err)
	}
	path, err := syscall.UTF16PtrFromString(link)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(path, syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Skipf("cannot create test junction: %v", err)
	}
	defer syscall.CloseHandle(handle)
	sub, _ := syscall.UTF16FromString(`\??\` + target)
	print, _ := syscall.UTF16FromString(target)
	buf := make([]byte, 16+2*(len(sub)+len(print)))
	binary.LittleEndian.PutUint32(buf[0:4], 0xa0000003) // IO_REPARSE_TAG_MOUNT_POINT
	binary.LittleEndian.PutUint16(buf[4:6], uint16(len(buf)-8))
	binary.LittleEndian.PutUint16(buf[10:12], uint16(2*(len(sub)-1)))
	binary.LittleEndian.PutUint16(buf[12:14], uint16(2*len(sub)))
	binary.LittleEndian.PutUint16(buf[14:16], uint16(2*(len(print)-1)))
	for i, c := range append(sub, print...) {
		binary.LittleEndian.PutUint16(buf[16+i*2:], c)
	}
	var returned uint32
	if err := syscall.DeviceIoControl(handle, 0x000900a4, &buf[0], uint32(len(buf)), nil, 0, &returned, nil); err != nil {
		t.Skipf("filesystem does not support test junctions: %v", err)
	}
}

func TestWindowsGuardRejectsJunctionEscape(t *testing.T) {
	home, _, outside := windowsFixture(t)
	windowsWrite(t, filepath.Join(outside, "keep.bin"))
	link := filepath.Join(home, "escape")
	windowsJunction(t, link, outside)
	for _, p := range []string{link, filepath.Join(link, "keep.bin")} {
		if _, err := (Guard{Home: home}).Check(p); err == nil {
			t.Errorf("junction path accepted: %q", p)
		}
	}
}

func TestWindowsCleanupDryRunAndNestedItems(t *testing.T) {
	home, _, _ := windowsFixture(t)
	parent := filepath.Join(home, "scratch")
	child := filepath.Join(parent, "data.bin")
	windowsWrite(t, child)
	res := &scan.Result{Categories: []scan.CategoryResult{{ID: "large-files", Items: []scan.Item{
		{ID: "parent", Path: parent, Size: 100, Kind: scan.KindDir},
		{ID: "child", Path: strings.ToUpper(child), Size: 100, Kind: scan.KindFile},
	}}}}
	for _, dryRun := range []bool{true, false} {
		out, err := Run(context.Background(), res, Request{Items: []string{"child", "parent"}, Mode: ModeDelete, DryRun: dryRun})
		if err != nil || out.Removed != 2 || out.Freed != 100 || len(out.Failed) != 0 {
			t.Fatalf("dryRun=%v: %+v %v", dryRun, out, err)
		}
		_, err = os.Stat(child)
		if dryRun && err != nil || !dryRun && !os.IsNotExist(err) {
			t.Fatalf("dryRun=%v child stat=%v", dryRun, err)
		}
	}
}

func TestWindowsRecycleBinRequiresExplicitDelete(t *testing.T) {
	windowsFixture(t)
	id := scan.ItemID("trash", scan.WindowsRecycleBinPath)
	it := scan.Item{ID: id, Path: scan.WindowsRecycleBinPath, Kind: scan.KindCommand, Category: "trash"}
	if handled, err := platformCommand(context.Background(), it, ModeTrash, true); !handled || err == nil {
		t.Fatal("default trash mode can empty Recycle Bin")
	}
	if handled, err := platformCommand(context.Background(), it, ModeDelete, true); !handled || err != nil {
		t.Fatalf("explicit delete dry-run refused: %v", err)
	}
	it.ID = "guessed"
	if _, err := platformCommand(context.Background(), it, ModeDelete, true); err == nil {
		t.Fatal("guessed Recycle Bin ID accepted")
	}
	if permanentRemoval(ModeTrash, scan.Item{Category: "trash"}) {
		t.Fatal("trash category silently implies permanent file deletion")
	}
}

func TestWindowsRecycleSinkRefusesPermanentRemoval(t *testing.T) {
	s := &recycleSink{refs: 1}
	if hr := sinkPreDelete(s, 0, nil); hr != hresultAbort || !s.rejected {
		t.Fatal("permanent deletion proposal accepted")
	}
	s = &recycleSink{refs: 1}
	if hr := sinkPreDelete(s, tsfDeleteRecycle, nil); hr != 0 || s.rejected {
		t.Fatal("recycle proposal refused")
	}
	sinkPostDelete(s, 0, nil, 0x80070005, nil)
	if !s.completed || hresultError("test", uintptr(s.result)) == nil {
		t.Fatal("per-item deletion error ignored")
	}
	var out *recycleSink
	if hr := sinkQuery(s, &iidProgressSink, &out); hr != 0 || out != s || s.refs != 2 {
		t.Fatal("progress sink QueryInterface failed")
	}
	if n := sinkRelease(s); n != 1 {
		t.Fatalf("sink reference count = %d", n)
	}
}

func TestWindowsCleanupDirectoryDoesNotFollowJunction(t *testing.T) {
	home, _, outside := windowsFixture(t)
	parent := filepath.Join(home, "scratch")
	windowsWrite(t, filepath.Join(outside, "keep.bin"))
	windowsWrite(t, filepath.Join(parent, "cache.bin"))
	windowsJunction(t, filepath.Join(parent, "linked"), outside)
	id := scan.ItemID("large-files", parent)
	res := &scan.Result{Categories: []scan.CategoryResult{{ID: "large-files", Items: []scan.Item{{ID: id, Path: parent, Size: 10, Kind: scan.KindDir}}}}}
	out, err := Run(context.Background(), res, Request{Items: []string{id}, Mode: ModeDelete})
	if err != nil || out.Removed != 1 || len(out.Failed) != 0 {
		t.Fatalf("cleanup failed: %+v %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.bin")); err != nil {
		t.Fatalf("junction target changed: %v", err)
	}
}

func TestWindowsGuardProtectsRedirectedUserFolders(t *testing.T) {
	home, _, outside := windowsFixture(t)
	downloads := filepath.Join(outside, "Downloads")
	windowsWrite(t, filepath.Join(downloads, "old.iso"))
	g := Guard{Home: home, UserRoots: []string{downloads}}
	if _, err := g.Check(filepath.Join(downloads, "old.iso")); err != nil {
		t.Fatalf("redirected user file refused: %v", err)
	}
	for _, p := range []string{downloads, outside} {
		if _, err := g.Check(p); err == nil {
			t.Errorf("redirected user root or ancestor accepted: %q", p)
		}
	}
}

func TestWindowsGuardDoesNotConfuseCaseSensitiveSibling(t *testing.T) {
	home, _, _ := windowsFixture(t)
	parent := filepath.Dir(home)
	path, _ := syscall.UTF16PtrFromString(parent)
	handle, err := syscall.CreateFile(path, syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Skipf("cannot enable case-sensitive test folder: %v", err)
	}
	defer syscall.CloseHandle(handle)
	flags := uint32(1)
	setInfo := syscall.NewLazyDLL("kernel32.dll").NewProc("SetFileInformationByHandle")
	ok, _, err := setInfo.Call(uintptr(handle), 23, uintptr(unsafe.Pointer(&flags)), 4) // FileCaseSensitiveInfo
	if ok == 0 {
		t.Skipf("case-sensitive directories unavailable: %v", err)
	}
	shadow := strings.TrimSuffix(home, filepath.Base(home)) + strings.ToUpper(filepath.Base(home))
	windowsWrite(t, filepath.Join(shadow, "keep.bin"))
	original, err := os.Stat(home)
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.Stat(shadow)
	if err != nil || os.SameFile(original, other) {
		t.Skip("filesystem did not create a distinct case-sensitive sibling")
	}
	if _, err := (Guard{Home: home}).Check(filepath.Join(shadow, "keep.bin")); err == nil {
		t.Fatal("distinct case-sensitive sibling accepted as home content")
	}
	if platformCovered(filepath.Join(shadow, "keep.bin"), home, true) {
		t.Fatal("dry-run counts distinct case-sensitive sibling as covered")
	}
}

func TestWindowsLeftoverGuardRequiresFreshFamilyAllowance(t *testing.T) {
	home, _, outside := windowsFixture(t)
	// No family directories exist yet, so the fresh dynamic query has no
	// allowance. This is also the result when registration is unknown or
	// the family has been reinstalled; scanner tests cover those API results.
	g := platformGuard(Guard{Home: home, TempRoots: []string{filepath.Join(home, "AppData", "Local")}}, scan.Item{Category: "app-leftovers"})
	if !g.RequireCacheRoot || len(g.CacheRoots) != 0 {
		t.Fatalf("unexpected leftover preflight: %+v", g)
	}
	family := filepath.Join(home, "AppData", "Local", "Packages", "Acme.Removed_abcdefghjkmnp")
	windowsWrite(t, filepath.Join(family, "LocalState", "data.bin"))
	if _, err := g.Check(family); err == nil {
		t.Fatal("broad temp root overrides an absent/reinstalled family allowance")
	}
	g.CacheRoots = []string{family}
	if _, err := g.Check(family); err != nil {
		t.Fatalf("current exact family allowance refused: %v", err)
	}
	g.CacheRoots = nil
	if _, err := g.Check(family); err == nil {
		t.Fatal("removed fresh family allowance is still accepted")
	}
	// An arbitrary scan category/path may not use the profile or configured
	// personal roots to bypass the native package-registration allowance.
	ordinary := filepath.Join(home, "scratch")
	windowsWrite(t, filepath.Join(ordinary, "data.bin"))
	if _, err := g.Check(ordinary); err == nil {
		t.Fatal("generic home grant bypasses leftover allowlist")
	}
	personal := filepath.Join(outside, "Documents")
	windowsWrite(t, filepath.Join(personal, "data.bin"))
	g.UserRoots = []string{personal}
	if _, err := g.Check(filepath.Join(personal, "data.bin")); err == nil {
		t.Fatal("redirected user-root grant bypasses leftover allowlist")
	}
}

func TestWindowsLeftoverGuardRedirectedAppDataRequiresFreshFamilyAllowance(t *testing.T) {
	home, _, outside := windowsFixture(t)
	local := filepath.Join(outside, "Local")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", local)
	g := platformGuard(Guard{Home: home, TempRoots: []string{local}, UserRoots: []string{outside}}, scan.Item{Category: "app-leftovers"})
	family := filepath.Join(local, "Packages", "Acme.Removed_abcdefghjkmnp")
	windowsWrite(t, filepath.Join(family, "LocalState", "data.bin"))
	if _, err := g.Check(family); err == nil {
		t.Fatal("redirected AppData/temp grants override missing live family allowance")
	}
	g.CacheRoots = []string{family}
	if _, err := g.Check(family); err != nil {
		t.Fatalf("current redirected family allowance refused: %v", err)
	}
	for _, protected := range []string{local, filepath.Join(local, "Packages")} {
		if _, err := g.Check(protected); err == nil {
			t.Errorf("package/container root accepted: %q", protected)
		}
	}
}
