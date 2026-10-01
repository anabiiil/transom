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
