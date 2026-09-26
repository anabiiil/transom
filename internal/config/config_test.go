package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryNewestFirstCapped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
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
	fi, err := os.Stat(filepath.Join(home, ".transom", "history.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("history file: %v %v", fi, err)
	}
}

func TestPrefs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	r, err := ProjectRoots()
	if err != nil || len(r) != 1 || r[0] != home {
		t.Fatalf("default = %v, %v", r, err)
	}
	for _, bad := range []string{`not json`, `["relative/dir"]`, `"/a"`} {
		if err := SetPref(PrefProjectRoots, bad); err == nil {
			t.Errorf("SetPref(%q) accepted", bad)
		}
	}
	if err := SetPref(PrefProjectRoots, `["~", "~/code", "/Volumes/Ext/Work/", ""]`); err != nil {
		t.Fatal(err)
	}
	r, err = ProjectRoots()
	want := []string{home, filepath.Join(home, "code"), "/Volumes/Ext/Work"}
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
