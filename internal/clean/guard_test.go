//go:build !windows

package clean

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realTemp is t.TempDir() with symlinks resolved (/var → /private/var).
func realTemp(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGuardCheck(t *testing.T) {
	home := realTemp(t)
	tmpRoot := realTemp(t)
	outside := realTemp(t) // neither home nor an allowed root

	mkdirs(t,
		filepath.Join(home, "Library", "Caches", "com.example.app"),
		filepath.Join(home, "Documents"),
		filepath.Join(tmpRoot, "job"),
		filepath.Join(outside, "secret"),
	)
	// ~/escape → /etc, ~/out → a dir outside every root.
	if err := os.Symlink("/etc", filepath.Join(home, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(home, "out")); err != nil {
		t.Fatal(err)
	}
	// A symlinked ancestor inside home that stays inside home is fine.
	if err := os.Symlink(filepath.Join(home, "Library", "Caches"), filepath.Join(home, "caches-link")); err != nil {
		t.Fatal(err)
	}

	g := Guard{Home: home, TempRoots: []string{tmpRoot}}

	cases := []struct {
		name string
		path string
		ok   bool
		want string // resolved path when ok ("" = same as path)
	}{
		{"home itself", home, false, ""},
		{"home with trailing slash", home + "/", false, ""},
		{"Documents", filepath.Join(home, "Documents"), false, ""},
		{"documents lowercase", filepath.Join(home, "documents"), false, ""},
		{"Library", filepath.Join(home, "Library"), false, ""},
		{"Library/Caches itself", filepath.Join(home, "Library", "Caches"), false, ""},
		{"Library/Preferences itself", filepath.Join(home, "Library", "Preferences"), false, ""},
		{"Desktop", filepath.Join(home, "Desktop"), false, ""},
		{"Downloads", filepath.Join(home, "Downloads"), false, ""},
		{"Pictures", filepath.Join(home, "Pictures"), false, ""},
		{"Movies", filepath.Join(home, "Movies"), false, ""},
		{"Music", filepath.Join(home, "Music"), false, ""},
		{"Applications", filepath.Join(home, "Applications"), false, ""},
		{"Public", filepath.Join(home, "Public"), false, ""},
		{"Trash folder itself", filepath.Join(home, ".Trash"), false, ""},
		{"root", "/", false, ""},
		{"etc", "/etc", false, ""},
		{"etc file", "/etc/hosts", false, ""},
		{"users dir", "/Users", false, ""},
		{"parent of home", filepath.Dir(home), false, ""},
		{"dotdot escaping", filepath.Join(home, "Library", "Caches") + "/../../Documents", false, ""},
		{"dotdot staying inside", filepath.Join(home, "Library", "Caches") + "/../Caches/com.example.app", false, ""},
		{"relative", "Library/Caches/x", false, ""},
		{"empty", "", false, ""},
		{"through symlink to /etc", filepath.Join(home, "escape", "hosts"), false, ""},
		{"through symlink outside", filepath.Join(home, "out", "file"), false, ""},
		{"outside dir", filepath.Join(outside, "secret"), false, ""},
		{"missing parent", filepath.Join(home, "nope", "x"), false, ""},
		{"temp root itself", tmpRoot, false, ""},
		{"parent of temp root", filepath.Dir(tmpRoot), false, ""},

		{"cache subdir", filepath.Join(home, "Library", "Caches", "com.example.app"), true, ""},
		{"file in Documents", filepath.Join(home, "Documents", "big.iso"), true, ""},
		{"symlink leaf itself", filepath.Join(home, "escape"), true, ""},
		{"inside temp root", filepath.Join(tmpRoot, "job"), true, ""},
		{"via in-home symlink", filepath.Join(home, "caches-link", "com.example.app"), true,
			filepath.Join(home, "Library", "Caches", "com.example.app")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := g.Check(c.path)
			if c.ok {
				if err != nil {
					t.Fatalf("Check(%q) = %v, want allowed", c.path, err)
				}
				want := c.want
				if want == "" {
					want = filepath.Clean(c.path)
				}
				if got != want {
					t.Fatalf("Check(%q) resolved to %q, want %q", c.path, got, want)
				}
				return
			}
			if err == nil {
				t.Fatalf("Check(%q) allowed (resolved %q), want refused", c.path, got)
			}
		})
	}
}

func TestGuardNoTempRoots(t *testing.T) {
	home := realTemp(t)
	other := realTemp(t)
	g := Guard{Home: home}
	if _, err := g.Check(filepath.Join(other, "x")); err == nil {
		t.Fatal("path outside home allowed without temp roots")
	}
}

func TestGuardEmptyHome(t *testing.T) {
	if _, err := (Guard{}).Check("/tmp/x"); err == nil || !strings.Contains(err.Error(), "home") {
		t.Fatalf("want home error, got %v", err)
	}
}

func TestDefaultGuardUsesHOME(t *testing.T) {
	home := realTemp(t)
	t.Setenv("HOME", home)
	g, err := DefaultGuard()
	if err != nil {
		t.Fatal(err)
	}
	if g.Home != home {
		t.Fatalf("Home = %q, want %q", g.Home, home)
	}
	if len(g.TempRoots) == 0 {
		t.Fatal("no temp roots")
	}
}

func TestUnsafeProjectRoot(t *testing.T) {
	cases := map[string]bool{ // root -> safe?
		"/":                          false,
		"/Volumes":                   false,
		"/Volumes/Ext":               false,
		"/Volumes/Ext/Work":          true,
		"/Volumes/Ext/Work/Projects": true,
		"/System":                    false,
		"/System/Volumes/Data/x":     false,
		"/Library/Developer":         false,
		"/Applications":              false,
		"/usr/local/src":             false,
		"/opt/homebrew":              false,
		"/private/var/folders":       false,
		"/var/tmp":                   false,
		"/etc":                       false,
		"/Users":                     false,
		"/Users/someone":             false,
		"/Users/someone/code":        true,
		"/Users/SomeOne/code":        true,
		"/usr":                       false,
		"/USR/local":                 false,
		"":                           false,
		"relative/dir":               false,
	}
	for root, safe := range cases {
		if err := unsafeProjectRoot(root); (err == nil) != safe {
			t.Errorf("unsafeProjectRoot(%q) = %v, want safe=%v", root, err, safe)
		}
	}
}

func TestCheckProjectRoot(t *testing.T) {
	roots := []string{"/Volumes/Ext/Work", "/", "/Volumes/Other", "relative"}
	cases := []struct {
		path string
		ok   bool
	}{
		{"/Volumes/Ext/Work/app/node_modules", true},
		{"/Volumes/Ext/Work/a/b/c/vendor", true},
		{"/Volumes/Ext/Work/py/.venv", true},
		{"/Volumes/Ext/Work/rs/target", true},
		{"/Volumes/Ext/Work/node_modules", true}, // strictly inside, dep name
		{"/Volumes/Ext/Work", false},             // the root itself
		{"/Volumes/Ext", false},                  // an ancestor
		{"/Volumes/Ext/Work/app", false},         // not a dep folder
		{"/Volumes/Ext/Work/app/src", false},
		{"/Volumes/Ext/Work/app/node_modules.bak", false},
		{"/Volumes/Ext/WorkOther/app/node_modules", false}, // prefix trick
		{"/Volumes/Other/app/node_modules", false},         // root is a volume root: ignored
		{"/etc/node_modules", false},                       // "/" root: ignored
		{"/usr/lib/node_modules", false},
	}
	for _, c := range cases {
		if err := checkProjectRoot(c.path, roots); (err == nil) != c.ok {
			t.Errorf("checkProjectRoot(%q) = %v, want ok=%v", c.path, err, c.ok)
		}
	}
	if err := checkProjectRoot("/Volumes/Ext/Work/app/node_modules", []string{"/Volumes/Ext/x/../Work"}); err == nil {
		t.Error("root with '..' accepted")
	}
}

// projectWorkDir returns a real directory outside $HOME and outside
// system folders to exercise Check end to end (read-only: Check only
// resolves parents). The repo checkout serves when it lives on an
// external volume; otherwise the test is skipped.
func projectWorkDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Skip(err)
	}
	wd, _ = filepath.EvalSymlinks(wd)
	root := filepath.Dir(wd)
	if unsafeProjectRoot(root) != nil || !strings.HasPrefix(root, "/Volumes/") {
		t.Skipf("checkout %s isn't on an external volume", wd)
	}
	return wd
}

func TestGuardProjectRootsEndToEnd(t *testing.T) {
	wd := projectWorkDir(t) // e.g. /Volumes/X/.../Cleaner/internal/clean
	home := realTemp(t)
	root := filepath.Dir(wd)
	g := Guard{Home: home, ProjectRoots: []string{root}}
	cases := []struct {
		path string
		ok   bool
	}{
		{filepath.Join(wd, "node_modules"), true},
		{filepath.Join(wd, "vendor"), true},
		{filepath.Join(wd, "guard.go"), false},
		{wd, false},
		{root, false},
		{wd + "/../clean/node_modules", false},             // '..'
		{filepath.Join(wd, "nope", "node_modules"), false}, // parent missing
	}
	for _, c := range cases {
		if _, err := g.Check(c.path); (err == nil) != c.ok {
			t.Errorf("Check(%q) = %v, want ok=%v", c.path, err, c.ok)
		}
	}
	// Without project roots the same dependency folder is refused.
	if _, err := (Guard{Home: home}).Check(filepath.Join(wd, "node_modules")); err == nil {
		t.Error("dependency folder outside home allowed without project roots")
	}
	// A symlink inside home pointing at the project root still only
	// reaches dependency folders.
	link := filepath.Join(home, "work")
	if err := os.Symlink(wd, link); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Check(filepath.Join(link, "guard.go")); err == nil {
		t.Error("source file reached through a symlink")
	}
	if got, err := g.Check(filepath.Join(link, "node_modules")); err != nil || got != filepath.Join(wd, "node_modules") {
		t.Errorf("dep folder through symlink: %q %v", got, err)
	}
}
