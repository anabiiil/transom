package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func testHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
		t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	}
	return home
}

func TestHistoryNewestFirstCapped(t *testing.T) {
	testHome(t)
	h, err := History()
	if err != nil || len(h) != 0 || h == nil {
		t.Fatalf("empty history = %v, %v", h, err)
	}
	for i := 0; i < MaxHistory+5; i++ {
		if err := AddHistory(HistoryEntry{Removed: i, Mode: "trash", Categories: []string{"trash"}}); err != nil {
			t.Fatal(err)
		}
	}
	h, err = History()
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != MaxHistory || h[0].Removed != MaxHistory+4 {
		t.Fatalf("len %d, newest %d", len(h), h[0].Removed)
	}
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "history.json"))
	if err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Fatalf("history file: %v %v", fi, err)
	}
}

func TestPrefs(t *testing.T) {
	testHome(t)
	if err := SetPref("", "x"); err == nil {
		t.Fatal("empty key accepted")
	}
	if err := SetPref("a", "1"); err != nil {
		t.Fatal(err)
	}
	if err := SetPref("b", "2"); err != nil {
		t.Fatal(err)
	}
	p, err := Prefs()
	if err != nil || p["a"] != "1" || p["b"] != "2" {
		t.Fatalf("prefs = %v, %v", p, err)
	}
}

func TestProjectRoots(t *testing.T) {
	home := testHome(t)
	r, err := ProjectRoots()
	if err != nil || len(r) != 1 || r[0] != home {
		t.Fatalf("default = %v, %v", r, err)
	}
	for _, bad := range []string{`not json`, `["relative/dir"]`, `"/a"`} {
		if err := SetPref(PrefProjectRoots, bad); err == nil {
			t.Errorf("SetPref(%q) accepted", bad)
		}
	}
	external := filepath.Join(home, "external", "Work")
	value, _ := json.Marshal([]string{"~", "~/code", external + string(filepath.Separator), ""})
	if err := SetPref(PrefProjectRoots, string(value)); err != nil {
		t.Fatal(err)
	}
	r, err = ProjectRoots()
	want := []string{home, filepath.Join(home, "code"), external}
	if err != nil || len(r) != len(want) {
		t.Fatalf("roots = %v, %v", r, err)
	}
	for i := range want {
		if r[i] != want[i] {
			t.Fatalf("roots = %v, want %v", r, want)
		}
	}
	// Clearing falls back to home.
	if err := SetPref(PrefProjectRoots, ""); err != nil {
		t.Fatal(err)
	}
	if r, _ := ProjectRoots(); len(r) != 1 || r[0] != home {
		t.Fatalf("cleared = %v", r)
	}
}

func TestConfigDirWindowsAndUnix(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, "redirected-local")
	for _, tc := range []struct{ goos, local, want string }{
		{"windows", local, filepath.Join(local, "Transom")},
		{"windows", "", filepath.Join(home, "AppData", "Local", "Transom")},
		{"windows", "relative", filepath.Join(home, "AppData", "Local", "Transom")},
		{"darwin", local, filepath.Join(home, ".transom")},
	} {
		if got := configDir(home, tc.goos, tc.local); got != tc.want {
			t.Errorf("configDir(%s, %q) = %q; want %q", tc.goos, tc.local, got, tc.want)
		}
	}
}

func TestPreferencesReplaceExistingFile(t *testing.T) {
	testHome(t)
	if err := SetPref("theme", "light"); err != nil {
		t.Fatal(err)
	}
	if err := SetPref("theme", "dark"); err != nil {
		t.Fatal(err)
	}
	prefs, err := Prefs()
	if err != nil || prefs["theme"] != "dark" {
		t.Fatalf("replacing existing preferences: %v, %v", prefs, err)
	}
	dir, _ := Dir()
	leftovers, err := filepath.Glob(filepath.Join(dir, ".config.json.*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary saves left behind: %v, %v", leftovers, err)
	}
}
