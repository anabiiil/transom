//go:build windows

// Transom's per-user installer uses only Windows' built-in APIs and tools.
// It is built separately from the portable app, with that app embedded.
package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"transom/internal/proc"
	"transom/internal/version"
)

const (
	installMarker = ".transom-install.json"
	installOwner  = "transom.windows.install.v1"
	uninstallKey  = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Transom`
	answerYes     = 6 // IDYES
)

var ownedFiles = []string{"Transom.exe", "transom-cli.exe", "Uninstall.exe", "README.md", "LICENSE", "THIRD_PARTY_NOTICES.txt"}

type installation struct {
	Owner   string `json:"owner"`
	Version string `json:"version"`
}

func main() {
	quiet := false
	uninstall := buildMode == "uninstall"
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--quiet":
			quiet = true
		case "--uninstall":
			uninstall = true
		default:
			show("Transom Setup", "Unknown setup option: "+arg, windows.MB_OK|windows.MB_ICONERROR)
			os.Exit(1)
		}
	}
	dir, shortcut, err := installPaths()
	if err == nil {
		if uninstall {
			err = removeInstallation(dir, shortcut, quiet)
		} else {
			err = install(dir, shortcut, quiet)
		}
	}
	if err != nil {
		if !quiet {
			show("Transom Setup", err.Error(), windows.MB_OK|windows.MB_ICONERROR)
		}
		os.Exit(1)
	}
}

func installPaths() (string, string, error) {
	local, err := os.UserCacheDir()
	if err != nil {
		return "", "", err
	}
	appData, err := os.UserConfigDir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(local, "Programs", "Transom"), filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Transom", "Transom.lnk"), nil
}

func install(dir, shortcut string, quiet bool) error {
	if !quiet && show("Transom Setup", "Install Transom "+version.Number+" for your Windows account?\n\nThe app and Start menu shortcut will be installed here:\n"+dir+"\n\nAdministrator access is not required.", windows.MB_YESNO|windows.MB_ICONQUESTION) != answerYes {
		return nil
	}
	if err := validateDestination(dir); err != nil {
		return err
	}
	// Check every payload before touching an existing installation.
	files := make(map[string][]byte, len(ownedFiles))
	var totalSize int
	for _, name := range ownedFiles {
		data, err := payloadFile(name)
		valid := err == nil && len(data) > 0
		if strings.HasSuffix(name, ".exe") {
			valid = valid && len(data) >= 2 && string(data[:2]) == "MZ"
		} else {
			valid = valid && utf8.Valid(data)
		}
		if !valid {
			return fmt.Errorf("the setup executable has an incomplete payload (%s). Download a complete Transom setup build", name)
		}
		files[name] = data
		totalSize += len(data)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating the installation folder: %w", err)
	}
	for _, name := range ownedFiles {
		if err := writeAtomic(filepath.Join(dir, name), files[name]); err != nil {
			return fmt.Errorf("installing %s: %w\n\nClose Transom and any transom-cli command, then run setup again", name, err)
		}
	}
	marker, _ := json.Marshal(installation{Owner: installOwner, Version: version.Number})
	if err := writeAtomic(filepath.Join(dir, installMarker), marker); err != nil {
		return err
	}
	if err := createShortcut(shortcut, dir); err != nil {
		return fmt.Errorf("Transom was copied to %s, but its Start menu shortcut could not be created: %w", dir, err)
	}
	if err := registerUninstaller(dir, totalSize); err != nil {
		return fmt.Errorf("registering Transom in Installed apps: %w", err)
	}
	if !quiet && show("Transom Setup", "Transom "+version.Number+" is installed.\n\nOpen Transom now?", windows.MB_YESNO|windows.MB_ICONINFORMATION) == answerYes {
		return shellOpen(filepath.Join(dir, "Transom.exe"))
	}
	return nil
}

// validateDestination prevents installation from replacing unrelated files
// in a pre-existing folder and refuses redirected target directories.
func validateDestination(dir string) error {
	if err := regularAncestors(dir); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || isReparsePoint(info) {
		return errors.New("the Transom installation folder must be a regular directory")
	}
	if err := verifyMarker(dir); err == nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("%s already contains files from an unrecognized installation. Move that folder aside before installing Transom", dir)
	}
	return nil
}

func verifyMarker(dir string) error {
	if err := regularAncestors(filepath.Join(dir, installMarker)); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || isReparsePoint(info) {
		return errors.New("the installation folder is missing or redirected")
	}
	data, err := os.ReadFile(filepath.Join(dir, installMarker))
	if err != nil {
		return err
	}
	var record installation
	if json.Unmarshal(data, &record) != nil || record.Owner != installOwner {
		return errors.New("this folder does not contain a recognized Transom installation")
	}
	return nil
}

func isReparsePoint(info os.FileInfo) bool {
	attrs, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return info.Mode()&os.ModeSymlink != 0 || (ok && attrs.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0)
}

// The target and each existing ancestor must be real paths: checking only
// the leaf would miss a junction in Programs or the Start menu directory.
func regularAncestors(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && isReparsePoint(info) {
			return fmt.Errorf("the path is redirected by a symbolic link or junction: %s", current)
		}
		if parent := filepath.Dir(current); parent == current {
			return nil
		}
	}
}

func writeAtomic(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".transom-setup-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

func createShortcut(shortcut, dir string) error {
	if err := regularAncestors(shortcut); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(shortcut), 0o700); err != nil {
		return err
	}
	script := "$ErrorActionPreference = 'Stop'; $shell = New-Object -ComObject WScript.Shell; $link = $shell.CreateShortcut(" + psQuote(shortcut) + "); $link.TargetPath = " + psQuote(filepath.Join(dir, "Transom.exe")) + "; $link.WorkingDirectory = " + psQuote(dir) + "; $link.IconLocation = " + psQuote(filepath.Join(dir, "Transom.exe")+",0") + "; $link.Description = 'Transom disk cleaner'; $link.Save()"
	command := powershell(script)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func registerUninstaller(dir string, bytes int) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	stringsToSet := map[string]string{
		"DisplayName":          "Transom",
		"DisplayVersion":       version.Number,
		"Publisher":            "Transom",
		"InstallLocation":      dir,
		"DisplayIcon":          filepath.Join(dir, "Transom.exe"),
		"UninstallString":      `"` + filepath.Join(dir, "Uninstall.exe") + `"`,
		"QuietUninstallString": `"` + filepath.Join(dir, "Uninstall.exe") + `" --quiet`,
		"InstallDate":          time.Now().Format("20060102"),
	}
	for name, value := range stringsToSet {
		if err := key.SetStringValue(name, value); err != nil {
			return err
		}
	}
	for name, value := range map[string]uint32{"NoModify": 1, "NoRepair": 1, "EstimatedSize": uint32(bytes / 1024)} {
		if err := key.SetDWordValue(name, value); err != nil {
			return err
		}
	}
	return nil
}

func removeInstallation(dir, shortcut string, quiet bool) error {
	if err := verifyMarker(dir); err != nil {
		return fmt.Errorf("Transom could not verify its installation: %w. No files were removed", err)
	}
	if err := regularAncestors(shortcut); err != nil {
		return err
	}
	if !quiet && show("Uninstall Transom", "Remove Transom from this Windows account?\n\nYour preferences, scan history, and any other files in the installation folder will be kept.", windows.MB_YESNO|windows.MB_ICONQUESTION) != answerYes {
		return nil
	}
	// Remove only known application files. Never recursively delete this
	// directory: users may have put unrelated files in it after installation.
	if err := removeApplicationFiles(dir); err != nil {
		return err
	}
	if err := os.Remove(shortcut); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing Transom's shortcut: %w", err)
	}
	_ = os.Remove(filepath.Dir(shortcut)) // only succeeds when empty
	if key, err := registry.OpenKey(registry.CURRENT_USER, uninstallKey, registry.QUERY_VALUE); err == nil {
		location, _, _ := key.GetStringValue("InstallLocation")
		key.Close()
		if strings.EqualFold(filepath.Clean(location), filepath.Clean(dir)) {
			if err := registry.DeleteKey(registry.CURRENT_USER, uninstallKey); err != nil {
				return err
			}
		}
	}
	// A running EXE cannot remove itself on Windows. A hidden helper waits
	// for this process to exit, deletes those two exact owned paths, then
	// attempts a non-recursive directory removal.
	script := "$ErrorActionPreference = 'Stop'; Wait-Process -Id " + fmt.Sprint(os.Getpid()) + " -ErrorAction SilentlyContinue; Start-Sleep -Milliseconds 500; $dir = " + psQuote(dir) + "; $marker = " + psQuote(filepath.Join(dir, installMarker)) + "; $cursor = $marker; while ($cursor) { if ([System.IO.File]::Exists($cursor) -or [System.IO.Directory]::Exists($cursor)) { if (([System.IO.File]::GetAttributes($cursor) -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) { exit 1 } }; $parent = [System.IO.Path]::GetDirectoryName($cursor); if ($parent -eq $cursor) { break }; $cursor = $parent }; $record = Get-Content -LiteralPath $marker -Raw | ConvertFrom-Json; if ($record.owner -ne " + psQuote(installOwner) + ") { exit 1 }; [System.IO.File]::Delete(" + psQuote(filepath.Join(dir, "Uninstall.exe")) + "); [System.IO.File]::Delete($marker); try { [System.IO.Directory]::Delete($dir, $false) } catch {}"
	helper := powershell(script)
	proc.Detach(helper)
	if err := helper.Start(); err != nil {
		return fmt.Errorf("finishing uninstall: %w", err)
	}
	_ = helper.Process.Release()
	if !quiet {
		show("Uninstall Transom", "Transom has been removed. Your preferences and history have been kept.", windows.MB_OK|windows.MB_ICONINFORMATION)
	}
	return nil
}

func removeApplicationFiles(dir string) error {
	if err := verifyMarker(dir); err != nil {
		return err
	}
	for _, name := range []string{"Transom.exe", "transom-cli.exe", "README.md", "LICENSE", "THIRD_PARTY_NOTICES.txt"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing %s: %w\n\nClose Transom and any transom-cli command, then run the uninstaller again", name, err)
		}
	}
	return nil
}

func powershell(script string) *exec.Cmd {
	utf16Script := utf16.Encode([]rune(script))
	encoded := make([]byte, len(utf16Script)*2)
	for index, value := range utf16Script {
		binary.LittleEndian.PutUint16(encoded[index*2:], value)
	}
	path := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	command := exec.Command(path, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded))
	proc.HideWindow(command)
	return command
}

func psQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func show(title, message string, flags uint32) int32 {
	caption, _ := windows.UTF16PtrFromString(title)
	text, _ := windows.UTF16PtrFromString(message)
	answer, _ := windows.MessageBox(0, text, caption, flags)
	return answer
}

func shellOpen(path string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}
