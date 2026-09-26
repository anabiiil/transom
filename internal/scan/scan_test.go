package scan

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"transom/internal/config"
)

func realTemp(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// fakeHome points $HOME at a fresh temp dir.
func fakeHome(t *testing.T) string {
	t.Helper()
	h := realTemp(t)
	t.Setenv("HOME", h)
	return h
}

func write(t *testing.T, p string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func randomBytes(t *testing.T, n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func setTime(t *testing.T, p string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
}

func TestSizeTree(t *testing.T) {
	dir := realTemp(t)
	write(t, filepath.Join(dir, "a.bin"), randomBytes(t, 100_000))
	write(t, filepath.Join(dir, "sub", "deep", "b.bin"), randomBytes(t, 50_000))
	// A hard link to a.bin must not be counted twice.
	if err := os.Link(filepath.Join(dir, "a.bin"), filepath.Join(dir, "sub", "a-link.bin")); err != nil {
		t.Fatal(err)
	}
	// A symlink to a big file elsewhere counts as the link only.
	other := realTemp(t)
	write(t, filepath.Join(other, "huge.bin"), randomBytes(t, 2_000_000))
	if err := os.Symlink(filepath.Join(other, "huge.bin"), filepath.Join(dir, "huge-link")); err != nil {
		t.Fatal(err)
	}

	sem := make(chan struct{}, parallelism)
	prog := NewProgress()
	size, newest := sizeTree(context.Background(), dir, sem, prog)
	if size < 150_000 {
		t.Fatalf("size %d < content 150000", size)
	}
	if size > 400_000 {
		t.Fatalf("size %d: hard link or symlink target counted", size)
	}
	if newest.IsZero() {
		t.Fatal("newest mtime not set")
	}
	if prog.Snapshot().Scanned == 0 {
		t.Fatal("progress not updated")
	}

	fsize, _ := sizeTree(context.Background(), filepath.Join(dir, "a.bin"), sem, nil)
	if fsize < 100_000 || fsize%512 != 0 {
		t.Fatalf("file size %d: want allocated bytes >= 100000", fsize)
	}
	if n, _ := sizeTree(context.Background(), filepath.Join(dir, "missing"), sem, nil); n != 0 {
		t.Fatalf("missing path size %d", n)
	}
}

func TestSizeTreeCancelled(t *testing.T) {
	dir := realTemp(t)
	write(t, filepath.Join(dir, "x", "y", "z.bin"), randomBytes(t, 10_000))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Must return promptly; only the root's own entry is counted.
	size, _ := sizeTree(ctx, dir, make(chan struct{}, 2), nil)
	if size > 10_000 {
		t.Fatalf("cancelled walk still descended: %d", size)
	}
}

func findCat(t *testing.T, res *Result, id string) CategoryResult {
	t.Helper()
	for _, c := range res.Categories {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("category %s missing", id)
	return CategoryResult{}
}

func TestDuplicates(t *testing.T) {
	home := fakeHome(t)
	content := randomBytes(t, 2<<20)
	older := filepath.Join(home, "Desktop", "copy.bin")
	newer := filepath.Join(home, "Documents", "orig.bin")
	third := filepath.Join(home, "Downloads", "dl", "again.bin")
	write(t, older, content)
	write(t, newer, content)
	write(t, third, content)
	setTime(t, older, time.Now().Add(-48*time.Hour))
	setTime(t, third, time.Now().Add(-72*time.Hour))
	setTime(t, newer, time.Now().Add(-1*time.Hour))

	// Same size, same first/last 64 KB, different middle: not a dup.
	tricky := append([]byte{}, content...)
	tricky[1<<20] ^= 0xff
	write(t, filepath.Join(home, "Documents", "tricky.bin"), tricky)
	// Same size, different content.
	write(t, filepath.Join(home, "Documents", "other.bin"), randomBytes(t, 2<<20))
	// Too small to matter even though identical.
	small := randomBytes(t, 1000)
	write(t, filepath.Join(home, "Desktop", "s1"), small)
	write(t, filepath.Join(home, "Desktop", "s2"), small)
	// Hard link: same inode, not a duplicate.
	if err := os.Link(filepath.Join(home, "Documents", "other.bin"), filepath.Join(home, "Desktop", "other-link.bin")); err != nil {
		t.Fatal(err)
	}
	// Inside node_modules: ignored.
	write(t, filepath.Join(home, "Documents", "proj", "package.json"), []byte("{}"))
	write(t, filepath.Join(home, "Documents", "proj", "node_modules", "x.bin"), content)

	res, err := Run(context.Background(), []string{"duplicates"}, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := findCat(t, res, "duplicates")
	if c.Count != 2 {
		for _, it := range c.Items {
			t.Logf("item %s (%s)", it.Path, it.Note)
		}
		t.Fatalf("want 2 duplicate items, got %d", c.Count)
	}
	got := map[string]Item{}
	for _, it := range c.Items {
		got[it.Path] = it
	}
	for _, p := range []string{older, third} {
		it, ok := got[p]
		if !ok {
			t.Fatalf("%s not reported", p)
		}
		if it.Risk != RiskCaution || it.Group == "" || !strings.Contains(it.Note, "Documents/orig.bin") {
			t.Fatalf("bad item %+v", it)
		}
	}
	if got[older].Group != got[third].Group {
		t.Fatal("copies of one file in different groups")
	}
	if _, ok := got[newer]; ok {
		t.Fatal("the newest copy must be kept, not listed")
	}
}

func TestStaleDeps(t *testing.T) {
	home := fakeHome(t)
	old := time.Now().AddDate(0, 0, -100)

	mkProject := func(rel string, files map[string]string, dep string, touched time.Time) string {
		p := filepath.Join(home, rel)
		for name, body := range files {
			write(t, filepath.Join(p, name), []byte(body))
		}
		write(t, filepath.Join(p, dep, "lib", "index.js"), randomBytes(t, 20_000))
		entries, _ := os.ReadDir(p)
		for _, e := range entries {
			if e.Name() != dep {
				setTime(t, filepath.Join(p, e.Name()), touched)
			}
		}
		return filepath.Join(p, dep)
	}

	staleNode := mkProject("code/old-app", map[string]string{"package.json": "{}", "index.js": ""}, "node_modules", old)
	mkProject("code/fresh-app", map[string]string{"package.json": "{}"}, "node_modules", time.Now())
	staleComposer := mkProject("code/php/site", map[string]string{"composer.json": "{}"}, "vendor", old)
	write(t, filepath.Join(staleComposer, "autoload.php"), []byte("<?php"))
	// Laravel's public/vendor (published assets) sits next to no
	// composer.json — and even with one, lacks vendor/autoload.php.
	laravel := filepath.Join(home, "code", "laravel")
	write(t, filepath.Join(laravel, "composer.json"), []byte("{}"))
	write(t, filepath.Join(laravel, "vendor", "autoload.php"), []byte("<?php"))
	write(t, filepath.Join(laravel, "public", "vendor", "livewire", "livewire.js"), randomBytes(t, 3000))
	write(t, filepath.Join(laravel, "public", "composer.json"), []byte("{}"))
	write(t, filepath.Join(laravel, "lang", "vendor", "pkg", "en.php"), randomBytes(t, 3000))
	for _, p := range []string{"composer.json", "vendor", "public", "lang"} {
		setTime(t, filepath.Join(laravel, p), old)
	}
	for _, p := range []string{"public/composer.json", "public/vendor/livewire/livewire.js", "lang/vendor/pkg/en.php"} {
		setTime(t, filepath.Join(laravel, p), old)
	}
	staleLaravel := filepath.Join(laravel, "vendor")
	// vendor without a manifest is somebody's folder, not deps.
	mkProject("code/notdeps", map[string]string{"README": ""}, "vendor", old)
	// node_modules without package.json: not reported.
	mkProject("code/orphan", map[string]string{"notes.txt": ""}, "node_modules", old)
	// Rust target with Cargo.toml.
	staleCargo := mkProject("code/rusty", map[string]string{"Cargo.toml": ""}, "target", old)
	write(t, filepath.Join(staleCargo, "CACHEDIR.TAG"), nil)
	// target next to Cargo.toml but without cargo's marker: not deps.
	mkProject("code/rusty-ish", map[string]string{"Cargo.toml": ""}, "target", old)
	// Under ~/Library and hidden dirs: pruned.
	mkProject("Library/proj", map[string]string{"package.json": "{}"}, "node_modules", old)
	mkProject(".hidden/proj", map[string]string{"package.json": "{}"}, "node_modules", old)
	// .venv with pyvenv.cfg.
	write(t, filepath.Join(home, "code", "py", "main.py"), nil)
	write(t, filepath.Join(home, "code", "py", ".venv", "pyvenv.cfg"), []byte("home = /usr/bin"))
	write(t, filepath.Join(home, "code", "py", ".venv", "lib", "x.py"), randomBytes(t, 5000))
	setTime(t, filepath.Join(home, "code", "py", "main.py"), old)
	staleVenv := filepath.Join(home, "code", "py", ".venv")

	res, err := Run(context.Background(), []string{"stale-deps"}, Options{StaleDays: 60}, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := findCat(t, res, "stale-deps")
	got := map[string]bool{}
	for _, it := range c.Items {
		got[it.Path] = true
		if it.Size <= 0 || it.Risk != RiskReview || it.Kind != KindDir {
			t.Fatalf("bad item %+v", it)
		}
	}
	want := []string{staleNode, staleComposer, staleCargo, staleVenv, staleLaravel}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing %s", w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d items, want %d: %v", len(got), len(want), got)
	}

	// With a larger threshold nothing is stale.
	res, err = Run(context.Background(), []string{"stale-deps"}, Options{StaleDays: 365}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c := findCat(t, res, "stale-deps"); c.Count != 0 {
		t.Fatalf("staleDays 365: got %d items", c.Count)
	}
}

func TestLargeFilesAndOldDownloads(t *testing.T) {
	home := fakeHome(t)
	big := filepath.Join(home, "Movies", "big.mov")
	write(t, big, randomBytes(t, 2_100_000))
	write(t, filepath.Join(home, "Library", "big-in-library.bin"), randomBytes(t, 2_100_000))
	write(t, filepath.Join(home, "small.txt"), []byte("hi"))

	oldDL := filepath.Join(home, "Downloads", "setup.dmg")
	write(t, oldDL, []byte("x"))
	write(t, filepath.Join(home, "Downloads", "new.pdf"), []byte("x"))
	setTime(t, oldDL, time.Now().AddDate(0, 0, -200))

	res, err := Run(context.Background(), []string{"large-files", "old-downloads"}, Options{LargeMinMB: 2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	lf := findCat(t, res, "large-files")
	if lf.Count != 1 || lf.Items[0].Path != big {
		t.Fatalf("large-files = %+v", lf.Items)
	}
	// old-downloads uses the newest of mtime/ctime/birth time; a file
	// just written has a fresh ctime, so it must NOT count as old.
	od := findCat(t, res, "old-downloads")
	if od.Count != 0 {
		t.Fatalf("freshly created file reported as old download: %+v", od.Items)
	}
}

func TestRunUnknownCategory(t *testing.T) {
	fakeHome(t)
	if _, err := Run(context.Background(), []string{"nope"}, Options{}, nil); err == nil {
		t.Fatal("want error")
	}
}

func TestRunCancelled(t *testing.T) {
	fakeHome(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, nil, Options{}, nil); err == nil {
		t.Fatal("want context error")
	}
}

func TestUserCachesSkipsOwnedAndApple(t *testing.T) {
	home := fakeHome(t)
	t.Setenv("GOCACHE", "")
	caches := filepath.Join(home, "Library", "Caches")
	write(t, filepath.Join(caches, "com.vendor.app", "c"), randomBytes(t, 5000))
	write(t, filepath.Join(caches, "com.apple.Safari", "c"), randomBytes(t, 5000))
	write(t, filepath.Join(caches, "CloudKit", "c"), randomBytes(t, 5000))
	write(t, filepath.Join(caches, "Homebrew", "c"), randomBytes(t, 5000))
	write(t, filepath.Join(caches, "pip", "c"), randomBytes(t, 5000))
	res, err := Run(context.Background(), []string{"user-caches", "package-caches"}, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	uc := findCat(t, res, "user-caches")
	if uc.Count != 1 || filepath.Base(uc.Items[0].Path) != "com.vendor.app" {
		t.Fatalf("user-caches = %+v", uc.Items)
	}
	pc := findCat(t, res, "package-caches")
	if pc.Count != 1 || pc.Items[0].Label != "pip" {
		t.Fatalf("package-caches = %+v", pc.Items)
	}
	if it := uc.Items[0]; it.ID != ItemID("user-caches", it.Path) || it.ModTime == "" {
		t.Fatalf("bad id/modTime: %+v", it)
	}
}

func TestResultWithoutAndTotals(t *testing.T) {
	r := &Result{Categories: []CategoryResult{
		{ID: "a", Items: []Item{{ID: "1", Path: "/x", Size: 10}, {ID: "2", Path: "/y", Size: 5}}},
		{ID: "b", Items: []Item{{ID: "3", Path: "/x", Size: 10}}}, // same path in two categories
	}}
	r.recompute()
	if r.TotalSize != 15 {
		t.Fatalf("total %d, want 15 (paths counted once)", r.TotalSize)
	}
	r2 := r.Without([]string{"2"})
	if r2.TotalSize != 10 || r2.Categories[0].Count != 1 || r.Categories[0].Count != 2 {
		t.Fatalf("Without: %+v / original %+v", r2, r)
	}
	if _, ok := r2.Lookup("2"); ok {
		t.Fatal("removed id still found")
	}
	if it, ok := r2.Lookup("3"); !ok || it.Category != "b" {
		t.Fatalf("Lookup(3) = %+v %v", it, ok)
	}
}

func TestLeftoverMatching(t *testing.T) {
	ia := &installedApps{vendors: map[string]bool{}, vendorNames: map[string]bool{}, names: map[string]bool{}}
	for _, id := range []string{"com.jetbrains.PhpStorm", "com.tinyspeck.slackmacgap", "org.videolan.vlc"} {
		ia.addID(id)
	}
	ia.names[normName("Visual Studio Code")] = true
	ia.names[normName("Slack")] = true

	cases := map[string]bool{
		"com.macpaw.CleanMyMac":                   true,
		"S8EX82NJP6.com.macpaw.CleanMyMac.Helper": true,
		"com.jetbrains.toolbox":                   false, // vendor installed
		"io.jetbrains.fleet":                      false, // vendor name installed
		"jetbrains.ps.262":                        false, // not a real TLD root
		"com.apple.Safari":                        false,
		"group.com.macpaw.x":                      false,
		"com.google.Chrome":                       false, // shared vendor list
		"com.acme.slack":                          false, // names an installed app
		"org.videolan.vlc.helper":                 false,
		"Google":                                  false, // not a bundle id
		"com.crashlytics.data":                    false,
		"com.example.visualstudiocode":            false,
		"net.someone.OldTool":                     true,
	}
	for id, want := range cases {
		if got := ia.isLeftover(id); got != want {
			t.Errorf("isLeftover(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestAppLeftoversNoInstalledAppsReportsNothing(t *testing.T) {
	// collectInstalled always finds /System/Applications on a Mac; this
	// only checks the scanner doesn't blow up on an empty ~/Library.
	fakeHome(t)
	if _, err := Run(context.Background(), []string{"app-leftovers"}, Options{}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestParsers(t *testing.T) {
	brew := []byte(`Would remove: /Users/x/Library/Caches/Homebrew/bison--3.8.2 (1.1MB)
Would remove: /opt/homebrew/Cellar/node/20.1.0 (2,345 files, 60.5MB)
Would remove: /opt/homebrew/Cellar/old/1.0 (512B)
Would remove: /Users/x/Library/Caches/Homebrew/bootsnap/abc (1,112 files, 10.4MB)
==> This operation would free approximately 72MB of disk space.`)
	got := brewReclaim(brew, "/Users/x/Library/Caches/Homebrew")
	want := int64(60.5*(1<<20)) + 512
	if got != want {
		t.Fatalf("brewReclaim = %d, want %d", got, want)
	}

	df := []byte(`{"Active":"1","Reclaimable":"1.5GB (60%)","Size":"2.5GB","TotalCount":"3","Type":"Images"}
{"Active":"0","Reclaimable":"120.4MB (100%)","Size":"120.4MB","TotalCount":"2","Type":"Containers"}
{"Active":"0","Reclaimable":"0B","Size":"0B","TotalCount":"0","Type":"Local Volumes"}
{"Active":"0","Reclaimable":"3.2kB","Size":"3.2kB","TotalCount":"4","Type":"Build Cache"}`)
	if got := dockerReclaim(df); got != 120_400_000+3_200 {
		t.Fatalf("dockerReclaim = %d", got)
	}

	sim := []byte(`{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-16-0":[{"udid":"A-B","name":"iPhone","isAvailable":false},{"udid":"../evil"}]}}`)
	ids, err := parseUnavailableDevices(sim)
	if err != nil || len(ids) != 1 || ids[0] != "A-B" {
		t.Fatalf("parseUnavailableDevices = %v, %v", ids, err)
	}
}

func TestCategoriesMatchContract(t *testing.T) {
	want := []string{"user-caches", "user-logs", "temp-files", "trash", "mail-downloads", "xcode",
		"simulators", "package-caches", "homebrew", "docker", "stale-deps", "large-files",
		"old-downloads", "duplicates", "app-leftovers"}
	cats := Categories()
	if len(cats) != len(want) {
		t.Fatalf("%d categories, want %d", len(cats), len(want))
	}
	groups := map[string]bool{"system": true, "developer": true, "projects": true, "files": true}
	risks := map[string]bool{RiskSafe: true, RiskReview: true, RiskCaution: true}
	for i, c := range cats {
		if c.ID != want[i] || !groups[c.Group] || !risks[c.Risk] || c.Icon == "" || c.Name == "" {
			t.Errorf("bad category %+v", c)
		}
	}
	for id := range Commands {
		if _, ok := LookupCategory(id); !ok {
			t.Errorf("command for unknown category %s", id)
		}
	}
}

// Tool noise must not make a project look recently worked on.
func TestLastTouchedIgnoresNoise(t *testing.T) {
	old := time.Now().AddDate(0, 0, -100)
	mk := func(t *testing.T) string {
		p := realTemp(t)
		write(t, filepath.Join(p, "package.json"), []byte("{}"))
		write(t, filepath.Join(p, "src", "app", "main.ts"), []byte("x"))
		write(t, filepath.Join(p, ".git", "HEAD"), []byte("ref: refs/heads/main"))
		write(t, filepath.Join(p, ".git", "refs", "heads", "main"), []byte("abc"))
		for _, f := range []string{"package.json", "src/app/main.ts", ".git/HEAD", ".git/refs/heads/main"} {
			setTime(t, filepath.Join(p, f), old)
		}
		return p
	}
	stale := func(p string) bool {
		lt := projectLastTouched(p)
		return !lt.IsZero() && lt.Before(time.Now().AddDate(0, 0, -60))
	}

	p := mk(t)
	if !stale(p) {
		t.Fatalf("baseline project not stale: %v", projectLastTouched(p))
	}
	// Noise: Finder, installs, git status/fetch, IDE state, dotenv,
	// build output, directory mtimes (all "now").
	write(t, filepath.Join(p, ".DS_Store"), nil)
	write(t, filepath.Join(p, "package-lock.json"), nil)
	write(t, filepath.Join(p, ".git", "index"), nil)
	write(t, filepath.Join(p, ".git", "FETCH_HEAD"), nil)
	write(t, filepath.Join(p, ".git", "refs", "remotes", "origin", "main"), nil)
	write(t, filepath.Join(p, ".idea", "workspace.xml"), nil)
	write(t, filepath.Join(p, ".env"), nil)
	write(t, filepath.Join(p, "dist", "bundle.js"), nil)
	write(t, filepath.Join(p, "storage", "logs", "laravel.log"), nil)
	write(t, filepath.Join(p, "node_modules", ".package-lock.json"), nil)
	if !stale(p) {
		t.Fatalf("noise made the project look fresh: %v", projectLastTouched(p))
	}

	// Real work: an edited source file a few levels down…
	p2 := mk(t)
	write(t, filepath.Join(p2, "src", "app", "main.ts"), []byte("edited"))
	if stale(p2) {
		t.Fatal("deep source edit not seen")
	}
	// …or a commit (branch ref moved).
	p3 := mk(t)
	write(t, filepath.Join(p3, ".git", "refs", "heads", "main"), []byte("def"))
	if stale(p3) {
		t.Fatal("new commit not seen")
	}
}

// The projectRoots preference is used when no roots are passed.
func TestStaleDepsUsesProjectRootsPref(t *testing.T) {
	home := fakeHome(t)
	ext := realTemp(t) // stands in for an external projects volume
	old := time.Now().AddDate(0, 0, -100)
	proj := filepath.Join(ext, "Work", "app")
	write(t, filepath.Join(proj, "package.json"), []byte("{}"))
	write(t, filepath.Join(proj, "node_modules", "x", "i.js"), randomBytes(t, 4000))
	setTime(t, filepath.Join(proj, "package.json"), old)
	// Also a stale project in home, which must NOT be found now.
	write(t, filepath.Join(home, "p", "package.json"), []byte("{}"))
	write(t, filepath.Join(home, "p", "node_modules", "y.js"), randomBytes(t, 4000))
	setTime(t, filepath.Join(home, "p", "package.json"), old)

	if err := config.SetPref(config.PrefProjectRoots, `["`+filepath.Join(ext, "Work")+`"]`); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), []string{"stale-deps"}, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := findCat(t, res, "stale-deps")
	if c.Count != 1 || c.Items[0].Path != filepath.Join(proj, "node_modules") {
		t.Fatalf("items = %+v", c.Items)
	}
	if len(res.Roots) != 1 || res.Roots[0] != filepath.Join(ext, "Work") {
		t.Fatalf("result roots = %v", res.Roots)
	}
	// An explicit root overrides the preference.
	res, err = Run(context.Background(), []string{"stale-deps"}, Options{Roots: []string{home}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c := findCat(t, res, "stale-deps"); c.Count != 1 || c.Items[0].Path != filepath.Join(home, "p", "node_modules") {
		t.Fatalf("override items = %+v", c.Items)
	}
}
