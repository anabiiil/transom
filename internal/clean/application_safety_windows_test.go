//go:build windows

package clean

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"transom/internal/scan"
)

func TestWindowsSavedScanCannotRemovePortableVSCode(t *testing.T) {
	home, temp, _ := windowsFixture(t)
	portable := filepath.Join(home, "Tools", "VSCode")
	appFile := filepath.Join(portable, "Code.exe")
	deps := filepath.Join(portable, "resources", "app", "node_modules")
	windowsWrite(t, appFile)
	windowsWrite(t, filepath.Join(deps, "dep", "index.js"))
	g := Guard{Home: home, TempRoots: []string{temp}, ProjectRoots: []string{filepath.Join(home, "Tools")}}
	for _, p := range []string{portable, appFile, deps, filepath.Dir(portable)} {
		if _, err := g.Check(p); err == nil {
			t.Errorf("portable application accepted: %s", p)
		}
	}
	res := &scan.Result{Roots: []string{filepath.Join(home, "Tools")}, Categories: []scan.CategoryResult{{ID: "stale-deps", Items: []scan.Item{{ID: scan.ItemID("stale-deps", deps), Path: deps, Kind: scan.KindDir, Size: 20}}}}}
	got, err := Run(context.Background(), res, Request{Items: []string{scan.ItemID("stale-deps", deps)}, Mode: ModeDelete})
	if err != nil {
		t.Fatal(err)
	}
	if got.Removed != 0 || len(got.Failed) != 1 {
		t.Fatalf("unsafe cleanup result: %+v", got)
	}
	if _, err := os.Stat(appFile); err != nil {
		t.Fatal("application was removed")
	}
}

func TestWindowsSavedTempScanCannotRemoveCustomEditorState(t *testing.T) {
	for _, mode := range []string{ModeTrash, ModeDelete} {
		t.Run(mode, func(t *testing.T) {
			_, temp, _ := windowsFixture(t)
			profile := filepath.Join(temp, "arbitrary-profile")
			state := filepath.Join(profile, "User", "globalStorage", "state.vscdb")
			backup := filepath.Join(profile, "Backups", "unsaved.txt")
			windowsWrite(t, state)
			windowsWrite(t, backup)
			res := &scan.Result{Categories: []scan.CategoryResult{{ID: "temp-files"}}}
			var ids []string
			for _, path := range []string{profile, state, backup} {
				id := scan.ItemID("temp-files", path)
				ids = append(ids, id)
				res.Categories[0].Items = append(res.Categories[0].Items, scan.Item{ID: id, Category: "temp-files", Path: path, Kind: scan.KindDir, Size: 20})
			}
			got, err := Run(context.Background(), res, Request{Items: ids, Mode: mode})
			if err != nil {
				t.Fatal(err)
			}
			if got.Removed != 0 || len(got.Failed) != len(ids) {
				t.Fatalf("unsafe custom profile cleanup: %+v", got)
			}
			for _, path := range []string{state, backup} {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("editor state was removed: %s", path)
				}
			}
		})
	}
}
