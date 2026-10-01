package scan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func fixtureInstalledApps() *installedApps {
	ia := &installedApps{vendors: map[string]bool{}, vendorNames: map[string]bool{}, names: map[string]bool{}}
	ia.addID("com.transomfixture.Installed")
	return ia
}

func fixtureLeftoverEnv(home string) *Env {
	return &Env{Home: home, sem: make(chan struct{}, parallelism), Prog: NewProgress()}
}

func TestAppLeftoversPreserveVisualStudioCodeData(t *testing.T) {
	home := realTemp(t)
	// VS Code's data name is Code, while its application bundle is
	// Visual Studio Code.app. Both stable and Insiders data must stay
	// protected even when the application lives outside standard roots.
	for _, rel := range []string{
		"Application Support/Code/User/settings.json",
		"Application Support/Code/User/workspaceStorage/state.vscdb",
		"Application Support/Code - Insiders/User/settings.json",
		"Application Support/com.microsoft.VSCode/data",
		"Caches/com.microsoft.VSCode/data",
		"Containers/com.microsoft.VSCode/data",
		"Preferences/com.microsoft.VSCode.plist",
		"Saved Application State/com.microsoft.VSCode.savedState/data",
	} {
		write(t, filepath.Join(home, "Library", filepath.FromSlash(rel)), []byte("keep"))
	}
	orphan := filepath.Join(home, "Library", "Application Support", "net.transomfixtureuninstalled.App")
	write(t, filepath.Join(orphan, "data"), []byte("review"))

	items, err := scanAppLeftoversWithInstalled(context.Background(), fixtureLeftoverEnv(home), fixtureInstalledApps())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Path != orphan {
		t.Fatalf("VS Code data must not be considered leftovers: %+v", items)
	}
}

func TestAppLeftoversPreserveAllDataWhenInventoryIncomplete(t *testing.T) {
	home := realTemp(t)
	for _, d := range leftoverDirs {
		p := filepath.Join(home, "Library", d.sub, "net.transomfixtureuninstalled.App"+d.suffix)
		if d.suffix == ".plist" {
			write(t, p, []byte("settings"))
		} else {
			write(t, filepath.Join(p, "data"), []byte("saved data"))
		}
	}
	ia := fixtureInstalledApps()
	ia.inventoryIncomplete = true
	items, err := scanAppLeftoversWithInstalled(context.Background(), fixtureLeftoverEnv(home), ia)
	if err != nil || len(items) != 0 {
		t.Fatalf("partial inventory must preserve app data: items=%+v, err=%v", items, err)
	}
	if ia.isLeftover("net.transomfixtureuninstalled.App") {
		t.Fatal("missing bundle identity is not proof of an uninstall")
	}
}

func TestFindAppsReportsIncompleteInventoryBeyondDepthLimit(t *testing.T) {
	root := realTemp(t)
	shallow := filepath.Join(root, "Utilities", "Visible.app")
	deep := filepath.Join(root, "Custom", "Tools", "Editors", "Microsoft", "Visual Studio Code.app")
	for _, p := range []string{shallow, deep} {
		write(t, filepath.Join(p, "Contents", "Info.plist"), []byte("fixture"))
	}
	apps := map[string]bool{}
	if findApps(root, 0, apps) {
		t.Fatal("an installed app beyond the enumeration bound must make the inventory incomplete")
	}
	if !apps[shallow] {
		t.Fatal("nested apps within the enumeration bound must still be recorded")
	}
	if apps[deep] {
		t.Fatal("fixture must exercise the enumeration bound")
	}
}

func TestFindAppsKeepsCaseInsensitiveApplicationBundles(t *testing.T) {
	root := realTemp(t)
	app := filepath.Join(root, "Editors", "Visual Studio Code.APP")
	write(t, filepath.Join(app, "Contents", "Info.plist"), []byte("fixture"))
	apps := map[string]bool{}
	if !findApps(root, 0, apps) || !apps[app] {
		t.Fatalf("installed .APP bundle missed: %+v", apps)
	}
	if !findApps(filepath.Join(root, "OptionalMissingRoot"), 0, apps) {
		t.Fatal("an absent optional root must not fail the inventory")
	}
	file := filepath.Join(root, "UnreadableAsDirectory")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if findApps(file, 0, apps) {
		t.Fatal("an unreadable app root must fail the inventory")
	}
}

func TestAppLeftoverValidationPreservesReinstalledAppData(t *testing.T) {
	home := realTemp(t)
	path := filepath.Join(home, "Library", "Application Support", "net.transomfixtureuninstalled.App")
	write(t, filepath.Join(path, "data"), []byte("saved data"))
	ia := fixtureInstalledApps()
	if err := validateAppLeftoverWithInstalled(home, path, ia); err != nil {
		t.Fatalf("fixture starts as an orphan: %v", err)
	}
	ia.addID("net.transomfixtureuninstalled.App")
	if err := validateAppLeftoverWithInstalled(home, path, ia); err == nil {
		t.Fatal("a reinstall after the scan must preserve its data during cleanup")
	}
	ia = fixtureInstalledApps()
	ia.inventoryIncomplete = true
	if err := validateAppLeftoverWithInstalled(home, path, ia); err == nil {
		t.Fatal("an incomplete cleanup-time inventory must preserve app data")
	}
	if err := validateAppLeftoverWithInstalled(home, filepath.Join(path, "data"), fixtureInstalledApps()); err == nil {
		t.Fatal("a file nested in app data is not a leftover entry")
	}
}

func TestFindAppsReportsLinkedInstallationDirectoryAsIncomplete(t *testing.T) {
	root := realTemp(t)
	custom := realTemp(t)
	write(t, filepath.Join(custom, "Installed.app", "Contents", "Info.plist"), []byte("fixture"))
	if err := os.Symlink(custom, filepath.Join(root, "CustomApps")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	apps := map[string]bool{}
	if findApps(root, 0, apps) {
		t.Fatal("apps in a linked installation directory must not appear uninstalled")
	}
	if len(apps) != 0 {
		t.Fatalf("installed-app discovery must not follow directory links: %+v", apps)
	}
}

func TestBundleIDsReportIncompleteInventoryWhenIdentityUnreadable(t *testing.T) {
	root := realTemp(t)
	known := filepath.Join(root, "Known.app")
	unknown := filepath.Join(root, "Visual Studio Code.app")
	for _, app := range []string{known, unknown} {
		write(t, filepath.Join(app, "Contents", "Info.plist"), []byte("fixture"))
	}
	ids, complete := bundleIDsWithReader(context.Background(), []string{known, unknown}, func(pl string) (string, error) {
		if pl == filepath.Join(known, "Contents", "Info.plist") {
			return "com.transomfixture.Known", nil
		}
		return "", fmt.Errorf("unreadable Info.plist fixture")
	})
	if complete || len(ids) != 1 || ids[0] != "com.transomfixture.Known" {
		t.Fatalf("one known bundle must not hide unreadable installed-app identities: ids=%v, complete=%v", ids, complete)
	}
	ids, complete = bundleIDsWithReader(context.Background(), []string{filepath.Join(root, "MissingIdentity.app")}, func(string) (string, error) {
		t.Error("missing Info.plist must not invoke the reader")
		return "", nil
	})
	if complete || len(ids) != 0 {
		t.Fatalf("missing bundle identity must fail closed: ids=%v, complete=%v", ids, complete)
	}
}

func TestBundleIDsRecognizeAppleSiliconWrappedApp(t *testing.T) {
	app := filepath.Join(realTemp(t), "Wrapped.app")
	plist := filepath.Join(app, "Wrapper", "Inner.app", "Info.plist")
	write(t, plist, []byte("fixture"))
	ids, complete := bundleIDsWithReader(context.Background(), []string{app}, func(pl string) (string, error) {
		if pl != plist {
			t.Errorf("unexpected bundle plist: %s", pl)
		}
		return "com.transomfixture.Wrapped", nil
	})
	if !complete || len(ids) != 1 || ids[0] != "com.transomfixture.Wrapped" {
		t.Fatalf("wrapped apps must have usable identities: ids=%v, complete=%v", ids, complete)
	}
}
