//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMarker(t *testing.T, dir string) {
	t.Helper()
	data, _ := json.Marshal(installation{Owner: installOwner, Version: "test"})
	if err := os.WriteFile(filepath.Join(dir, installMarker), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDestinationRefusesUnrelatedExistingFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "family-photo.jpg")
	if err := os.WriteFile(path, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateDestination(dir); err == nil {
		t.Fatal("installation accepted an unrecognized nonempty directory")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "keep me" {
		t.Fatal("destination validation changed an unrelated file")
	}
}

func TestDestinationAcceptsEmptyOrOwnedInstallation(t *testing.T) {
	dir := t.TempDir()
	if err := validateDestination(dir); err != nil {
		t.Fatal(err)
	}
	writeMarker(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateDestination(dir); err != nil {
		t.Fatalf("recognized upgrade refused: %v", err)
	}
}

func TestMissingOrInvalidMarkerCannotAuthorizeRemoval(t *testing.T) {
	dir := t.TempDir()
	for _, content := range []string{"", "not json", `{"owner":"another-app"}`} {
		if content != "" {
			if err := os.WriteFile(filepath.Join(dir, installMarker), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := verifyMarker(dir); err == nil {
			t.Fatalf("invalid marker accepted: %q", content)
		}
		if err := removeApplicationFiles(dir); err == nil {
			t.Fatalf("invalid marker authorized removal: %q", content)
		}
	}
}

func TestApplicationRemovalPreservesUnknownContents(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir)
	for _, name := range []string{"Transom.exe", "transom-cli.exe", "Uninstall.exe", "README.md", "LICENSE", "THIRD_PARTY_NOTICES.txt", "personal-file.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeApplicationFiles(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Transom.exe", "transom-cli.exe", "README.md", "LICENSE", "THIRD_PARTY_NOTICES.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("owned application file remained: %s", name)
		}
	}
	for _, name := range []string{"Uninstall.exe", "personal-file.txt", installMarker} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("unrelated/deferred file was removed: %s: %v", name, err)
		}
	}
}

func TestAtomicUpdateReplacesOnlyOwnedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Transom.exe")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatalf("update failed: %q, %v", data, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("update left temporary files: %v", entries)
	}
}

func TestPowerShellPathQuotesRemainLiteral(t *testing.T) {
	value := `C:\Users\O'Brien\$(Write-Error injection); test.txt`
	quoted := psQuote(value)
	if !strings.Contains(quoted, "O''Brien") || quoted[0] != '\'' || quoted[len(quoted)-1] != '\'' {
		t.Fatalf("incorrect PowerShell literal: %s", quoted)
	}
	command := powershell("$value = " + quoted + "; [Console]::Write($value)")
	output, err := command.CombinedOutput()
	if err != nil || string(output) != value {
		t.Fatalf("quoted path executed or changed: %q, %v", output, err)
	}
}

func TestRedirectedDestinationAncestorIsRefused(t *testing.T) {
	base := t.TempDir()
	target := t.TempDir()
	link := filepath.Join(base, "redirected")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("creating Windows symlinks is unavailable: %v", err)
	}
	if err := validateDestination(filepath.Join(link, "Transom")); err == nil {
		t.Fatal("symbolic-link ancestor accepted")
	}
}
