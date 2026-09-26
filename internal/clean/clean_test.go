package clean

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"transom/internal/config"
	"transom/internal/scan"
)

// fakeHome points $HOME at a fresh temp dir so nothing real is touched.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := realTemp(t)
	t.Setenv("HOME", home)
	return home
}

func writeFile(t *testing.T, p string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

type fixture struct {
	home string
	res  *scan.Result
	ids  map[string]string // short name -> item id
}

// newFixture builds a home with a few cache dirs, a trashed file and a
// scan result that lists them.
func newFixture(t *testing.T) *fixture {
	home := fakeHome(t)
	caches := filepath.Join(home, "Library", "Caches")
	writeFile(t, filepath.Join(caches, "app1", "a.bin"), 4000)
	writeFile(t, filepath.Join(caches, "app2", "b.bin"), 3000)
	writeFile(t, filepath.Join(caches, "loose.log"), 1000)
	writeFile(t, filepath.Join(home, ".Trash", "old.txt"), 500)

	f := &fixture{home: home, ids: map[string]string{}}
	item := func(cat, short, path string, size int64, kind string) scan.Item {
		id := scan.ItemID(cat, path)
		f.ids[short] = id
		return scan.Item{ID: id, Path: path, Label: short, Size: size, Kind: kind, Risk: scan.RiskSafe}
	}
	f.res = &scan.Result{Categories: []scan.CategoryResult{
		{ID: "user-caches", Items: []scan.Item{
			item("user-caches", "app1", filepath.Join(caches, "app1"), 4096, scan.KindDir),
			item("user-caches", "app2", filepath.Join(caches, "app2"), 3072, scan.KindDir),
			item("user-caches", "loose", filepath.Join(caches, "loose.log"), 1024, scan.KindFile),
			item("user-caches", "documents", filepath.Join(home, "Documents"), 1, scan.KindDir),
		}},
		{ID: "trash", Items: []scan.Item{
			item("trash", "old", filepath.Join(home, ".Trash", "old.txt"), 512, scan.KindFile),
		}},
	}}
	return f
}

func history(t *testing.T) []config.HistoryEntry {
	t.Helper()
	h, err := config.History()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestCleanTrashMode(t *testing.T) {
	f := newFixture(t)
	app1 := filepath.Join(f.home, "Library", "Caches", "app1")
	res, err := Run(context.Background(), f.res, Request{Items: []string{f.ids["app1"], f.ids["loose"]}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 2 || res.Freed != 4096+1024 || len(res.Failed) != 0 || res.DryRun {
		t.Fatalf("unexpected result %+v", res)
	}
	if exists(app1) {
		t.Fatal("app1 still in place")
	}
	if !exists(filepath.Join(f.home, ".Trash", "app1", "a.bin")) {
		t.Fatal("app1 not moved into ~/.Trash")
	}
	if !exists(filepath.Join(f.home, ".Trash", "loose.log")) {
		t.Fatal("loose.log not moved into ~/.Trash")
	}
	h := history(t)
	if len(h) != 1 || h[0].Mode != ModeTrash || h[0].Removed != 2 || h[0].Freed != 5120 ||
		len(h[0].Categories) != 1 || h[0].Categories[0] != "user-caches" {
		t.Fatalf("history = %+v", h)
	}
}

func TestCleanTrashNameCollision(t *testing.T) {
	f := newFixture(t)
	// Something called app2 is already in the Trash.
	writeFile(t, filepath.Join(f.home, ".Trash", "app2"), 10)
	res, err := Run(context.Background(), f.res, Request{Items: []string{f.ids["app2"]}, Mode: ModeTrash})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 {
		t.Fatalf("result %+v", res)
	}
	entries, _ := os.ReadDir(filepath.Join(f.home, ".Trash"))
	var moved string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "app2 ") {
			moved = e.Name()
		}
	}
	if moved == "" {
		t.Fatalf("no suffixed app2 in Trash: %v", entries)
	}
	if !exists(filepath.Join(f.home, ".Trash", moved, "b.bin")) {
		t.Fatalf("moved dir %q lacks its content", moved)
	}
	if fi, err := os.Stat(filepath.Join(f.home, ".Trash", "app2")); err != nil || fi.IsDir() {
		t.Fatal("pre-existing Trash item was overwritten")
	}
}

func TestTrashNameDotfileAndExt(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 26, 14, 3, 7, 0, time.UTC)
	writeFile(t, filepath.Join(dir, "a.txt"), 1)
	writeFile(t, filepath.Join(dir, ".env"), 1)
	if got := filepath.Base(trashName(dir, "a.txt", now)); got != "a 2026-09-26 14.03.07.txt" {
		t.Fatalf("got %q", got)
	}
	if got := filepath.Base(trashName(dir, ".env", now)); got != ".env 2026-09-26 14.03.07" {
		t.Fatalf("got %q", got)
	}
	if got := filepath.Base(trashName(dir, "new.txt", now)); got != "new.txt" {
		t.Fatalf("got %q", got)
	}
}

func TestCleanDeleteMode(t *testing.T) {
	f := newFixture(t)
	app1 := filepath.Join(f.home, "Library", "Caches", "app1")
	res, err := Run(context.Background(), f.res, Request{Items: []string{f.ids["app1"]}, Mode: ModeDelete})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 || res.Freed != 4096 {
		t.Fatalf("result %+v", res)
	}
	if exists(app1) || exists(filepath.Join(f.home, ".Trash", "app1")) {
		t.Fatal("app1 should be gone for good")
	}
	if h := history(t); len(h) != 1 || h[0].Mode != ModeDelete {
		t.Fatalf("history = %+v", h)
	}
}

func TestCleanDryRunTouchesNothing(t *testing.T) {
	f := newFixture(t)
	all := []string{f.ids["app1"], f.ids["app2"], f.ids["loose"], f.ids["old"]}
	res, err := Run(context.Background(), f.res, Request{Items: all, Mode: ModeDelete, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.Removed != 4 || res.Freed != 4096+3072+1024+512 {
		t.Fatalf("result %+v", res)
	}
	for _, p := range []string{
		filepath.Join(f.home, "Library", "Caches", "app1", "a.bin"),
		filepath.Join(f.home, "Library", "Caches", "app2", "b.bin"),
		filepath.Join(f.home, "Library", "Caches", "loose.log"),
		filepath.Join(f.home, ".Trash", "old.txt"),
	} {
		if !exists(p) {
			t.Fatalf("dry run removed %s", p)
		}
	}
	if exists(filepath.Join(f.home, ".transom", "history.json")) {
		t.Fatal("dry run wrote history")
	}
}

func TestCleanUnknownIDRejected(t *testing.T) {
	f := newFixture(t)
	_, err := Run(context.Background(), f.res, Request{Items: []string{f.ids["app1"], "deadbeef0000"}})
	if err == nil || !strings.Contains(err.Error(), "unknown item") {
		t.Fatalf("err = %v", err)
	}
	if !exists(filepath.Join(f.home, "Library", "Caches", "app1")) {
		t.Fatal("a request with an unknown id must not touch anything")
	}
}

func TestCleanNoScanAndBadMode(t *testing.T) {
	if _, err := Run(context.Background(), nil, Request{Items: []string{"x"}}); err == nil {
		t.Fatal("want error without a scan")
	}
	f := newFixture(t)
	if _, err := Run(context.Background(), f.res, Request{Items: []string{f.ids["app1"]}, Mode: "shred"}); err == nil {
		t.Fatal("want error for unknown mode")
	}
	if _, err := Run(context.Background(), f.res, Request{}); err == nil {
		t.Fatal("want error for empty selection")
	}
}

func TestCleanTrashCategoryDeletesPermanently(t *testing.T) {
	f := newFixture(t)
	old := filepath.Join(f.home, ".Trash", "old.txt")
	res, err := Run(context.Background(), f.res, Request{Items: []string{f.ids["old"]}, Mode: ModeTrash})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 || exists(old) {
		t.Fatalf("trash item not deleted: %+v", res)
	}
	entries, _ := os.ReadDir(filepath.Join(f.home, ".Trash"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "old") {
			t.Fatalf("trash item was re-trashed as %q", e.Name())
		}
	}
}

func TestCleanGuardRefusalIsReported(t *testing.T) {
	f := newFixture(t)
	mkdirs(t, filepath.Join(f.home, "Documents"))
	res, err := Run(context.Background(), f.res, Request{Items: []string{f.ids["documents"]}, Mode: ModeDelete})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 0 || len(res.Failed) != 1 || res.Failed[0].ID != f.ids["documents"] {
		t.Fatalf("result %+v", res)
	}
	if !exists(filepath.Join(f.home, "Documents")) {
		t.Fatal("~/Documents was removed")
	}
	// Failed must marshal as [] not null.
	b, _ := json.Marshal(&Result{Failed: []Failure{}})
	if !strings.Contains(string(b), `"failed":[]`) {
		t.Fatalf("json = %s", b)
	}
}

func TestCleanSymlinkRemovesLinkNotTarget(t *testing.T) {
	f := newFixture(t)
	target := realTemp(t)
	writeFile(t, filepath.Join(target, "keep.txt"), 100)
	link := filepath.Join(f.home, "Library", "Caches", "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	id := scan.ItemID("user-caches", link)
	f.res.Categories[0].Items = append(f.res.Categories[0].Items,
		scan.Item{ID: id, Path: link, Size: 1, Kind: scan.KindFile, Risk: scan.RiskSafe})
	res, err := Run(context.Background(), f.res, Request{Items: []string{id}, Mode: ModeDelete})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 || exists(link) {
		t.Fatalf("link not removed: %+v", res)
	}
	if !exists(filepath.Join(target, "keep.txt")) {
		t.Fatal("symlink target content was deleted")
	}
}

func TestCleanMissingItemFails(t *testing.T) {
	f := newFixture(t)
	os.RemoveAll(filepath.Join(f.home, "Library", "Caches", "app1"))
	res, err := Run(context.Background(), f.res, Request{Items: []string{f.ids["app1"]}, Mode: ModeDelete})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 0 || len(res.Failed) != 1 || !strings.Contains(res.Failed[0].Error, "no longer exists") {
		t.Fatalf("result %+v", res)
	}
	if exists(filepath.Join(f.home, ".transom", "history.json")) {
		t.Fatal("history written though nothing was removed")
	}
}

func TestCleanNestedItemsCountedOnce(t *testing.T) {
	f := newFixture(t)
	caches := filepath.Join(f.home, "Library", "Caches")
	inner := filepath.Join(caches, "app1", "a.bin")
	id := scan.ItemID("duplicates", inner)
	f.res.Categories = append(f.res.Categories, scan.CategoryResult{ID: "duplicates", Items: []scan.Item{
		{ID: id, Path: inner, Size: 4096, Kind: scan.KindFile, Risk: scan.RiskCaution},
	}})
	res, err := Run(context.Background(), f.res, Request{Items: []string{id, f.ids["app1"]}, Mode: ModeDelete})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 2 || res.Freed != 4096 || len(res.Failed) != 0 {
		t.Fatalf("result %+v", res)
	}
}

// A stale-deps item under the scan's project roots passes the guard
// (dry run; the folder doesn't exist so it's reported as gone), while
// the same path in any other category is refused.
func TestCleanProjectRootsOnlyForStaleDeps(t *testing.T) {
	wd := projectWorkDir(t)
	fakeHome(t)
	p := filepath.Join(wd, "node_modules")
	res := &scan.Result{Roots: []string{filepath.Dir(wd)}, Categories: []scan.CategoryResult{
		{ID: "stale-deps", Items: []scan.Item{{ID: "sd", Path: p, Size: 1, Kind: scan.KindDir}}},
		{ID: "large-files", Items: []scan.Item{{ID: "lf", Path: p, Size: 1, Kind: scan.KindDir}}},
	}}
	out, err := Run(context.Background(), res, Request{Items: []string{"sd", "lf"}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	errs := map[string]string{}
	for _, f := range out.Failed {
		errs[f.ID] = f.Error
	}
	if !strings.Contains(errs["sd"], "no longer exists") {
		t.Errorf("stale-deps item: %q (want guard to pass)", errs["sd"])
	}
	if !strings.Contains(errs["lf"], "outside the home folder") {
		t.Errorf("large-files item: %q (want guard refusal)", errs["lf"])
	}
}

// Exercises the real Finder fallback: moves a tiny, clearly named temp
// file to the user's Trash. Opt-in (it drives Finder via Apple Events and
// may prompt for automation permission): TRANSOM_TEST_FINDER=1.
func TestFinderTrash(t *testing.T) {
	if os.Getenv("TRANSOM_TEST_FINDER") != "1" {
		t.Skip("set TRANSOM_TEST_FINDER=1 to run (moves a temp file to your Trash)")
	}
	name := fmt.Sprintf("transom-test-%d.txt", time.Now().UnixNano())
	p := filepath.Join(realTemp(t), name)
	writeFile(t, p, 16)
	if err := finderTrash(p); err != nil {
		t.Fatal(err)
	}
	if exists(p) {
		t.Fatal("file still in place after Finder delete")
	}
	t.Logf("moved %s to the Trash", name)
}

func TestVolumeTrashDir(t *testing.T) {
	// The boot volume isn't "another volume".
	if _, err := volumeTrashDir(filepath.Join(realTemp(t), "x")); err == nil {
		t.Error("boot volume treated as a separate volume")
	}
	// On an external checkout, the volume's .Trashes/<uid> is found
	// when it exists (read-only check).
	wd := projectWorkDir(t)
	dir, err := volumeTrashDir(filepath.Join(wd, "node_modules"))
	if err != nil {
		t.Skipf("no per-user trash on this volume: %v", err)
	}
	if !strings.HasPrefix(dir, "/Volumes/") || !strings.Contains(dir, "/.Trashes/") {
		t.Fatalf("volume trash = %q", dir)
	}
}
