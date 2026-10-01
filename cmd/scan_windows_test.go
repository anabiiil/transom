//go:build windows

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"transom/internal/scan"
)

func TestWindowsRootsExpandHomeAndKeepAbsolutePaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{`~\Projects`, "~/Projects"} {
		opts := scan.Options{Roots: []string{input}}
		if err := absRoots(&opts); err != nil {
			t.Fatal(err)
		}
		if opts.Roots[0] != filepath.Join(home, "Projects") {
			t.Fatalf("root %q expanded to %q", input, opts.Roots[0])
		}
	}
	root := filepath.Join(t.TempDir(), "Project with spaces")
	opts := scan.Options{Roots: []string{root}}
	if err := absRoots(&opts); err != nil || opts.Roots[0] != root {
		t.Fatalf("absolute root changed: %q, %v", opts.Roots[0], err)
	}
}
