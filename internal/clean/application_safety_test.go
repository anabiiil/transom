//go:build !windows

package clean

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"transom/internal/scan"
)

func TestSavedCacheScanCannotRemoveVSCode(t *testing.T) {
	for _, mode := range []string{ModeTrash, ModeDelete} {
		t.Run(mode, func(t *testing.T) {
			home := fakeHome(t)
			caches := filepath.Join(home, "Library", "Caches")
			app := filepath.Join(caches, "unknown-update-cache", "Visual Studio Code.app")
			appFile := filepath.Join(app, "Contents", "Resources", "app", "node_modules", "dep", "index.js")
			shipit := filepath.Join(caches, "com.microsoft.VSCode.ShipIt")
			state := filepath.Join(home, "Library", "Application Support", "Code", "User", "settings.json")
			customProfile := filepath.Join(caches, "custom-editor-data")
			customState := filepath.Join(customProfile, "User", "workspaceStorage", "workspace", "state.vscdb")
			disposable := filepath.Join(caches, "thumbnails", "cache.bin")
			for _, p := range []string{appFile, filepath.Join(shipit, "ShipItState.plist"), state, customState, disposable} {
				writeFile(t, p, 20)
			}
			protected := []string{app, appFile, filepath.Dir(app), shipit, state, customProfile, customState}
			res := &scan.Result{Categories: []scan.CategoryResult{{ID: "user-caches"}}}
			var ids []string
			for _, p := range append(protected, disposable) {
				id := scan.ItemID("user-caches", p)
				ids = append(ids, id)
				res.Categories[0].Items = append(res.Categories[0].Items, scan.Item{ID: id, Path: p, Kind: scan.KindDir, Size: 20})
			}
			got, err := Run(context.Background(), res, Request{Items: ids, Mode: mode})
			if err != nil {
				t.Fatal(err)
			}
			if got.Removed != 1 || len(got.Failed) != len(protected) {
				t.Fatalf("unsafe cleanup result: %+v", got)
			}
			for _, p := range []string{appFile, filepath.Join(shipit, "ShipItState.plist"), state, customState} {
				if _, err := os.Stat(p); err != nil {
					t.Errorf("protected data removed: %s", p)
				}
			}
			if exists(disposable) {
				t.Fatal("ordinary cache was not cleaned")
			}
		})
	}
}

func TestGuardPreservesPortableAppsAndTheirParents(t *testing.T) {
	home := fakeHome(t)
	app := filepath.Join(home, "Downloads", "old-directory", "Visual Studio Code.APP")
	file := filepath.Join(app, "Contents", "MacOS", "Electron")
	writeFile(t, file, 20)
	portable := filepath.Join(home, "Tools", "VSCode")
	writeFile(t, filepath.Join(portable, "Code.exe"), 20)
	deps := filepath.Join(portable, "resources", "app", "node_modules")
	writeFile(t, filepath.Join(deps, "dep", "index.js"), 20)
	g := Guard{Home: home, ProjectRoots: []string{filepath.Join(home, "Tools")}}
	for _, p := range []string{app, file, filepath.Dir(app), portable, deps, filepath.Join(home, ".vscode", "extensions")} {
		if _, err := g.Check(p); err == nil {
			t.Errorf("guard accepted installed app/state: %s", p)
		}
	}
}
