//go:build windows

package cleanup

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsPurgePathsAreExactAndScoped(t *testing.T) {
	programData := filepath.Join(t.TempDir(), "ProgramData")
	t.Setenv("ProgramData", programData)
	config := filepath.Join(programData, "PrintMaster", "agent", "config.toml")
	data := filepath.Join(programData, "PrintMaster", "agent")
	if !isKnownWindowsConfigPath(config) {
		t.Fatalf("known Agent config was rejected: %s", config)
	}
	if isKnownWindowsConfigPath(filepath.Join(programData, "PrintMaster", "config")) {
		t.Fatal("directory or unrecognized config path was allowed")
	}
	if !isKnownWindowsDataPath(data) {
		t.Fatalf("known Agent data path was rejected: %s", data)
	}
	if isKnownWindowsDataPath(filepath.Join(programData, "PrintMaster")) {
		t.Fatal("shared PrintMaster data root must not be purgeable")
	}
}

func TestWindowsServiceExecutableParsesQuotedPathAndArguments(t *testing.T) {
	got, err := windowsServiceExecutable(`"C:\Program Files\PrintMaster\Agent\printmaster-agent.exe" --service run`)
	if err != nil {
		t.Fatal(err)
	}
	want := `C:\Program Files\PrintMaster\Agent\printmaster-agent.exe`
	if got != want {
		t.Fatalf("service executable = %q, want %q", got, want)
	}
}

func TestKnownBootstrapUninstallEntryRequiresExactIdentity(t *testing.T) {
	programData := `C:\ProgramData`
	executable := filepath.Join(programData, "PrintMaster", "bin", "printmaster-agent.exe")
	entry := windowsUninstallEntry{
		DisplayName:           "PrintMaster Agent",
		RegistryHive:          "HKLM",
		RegistryPath:          `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\PrintMasterAgent`,
		Publisher:             "PrintMaster",
		InstallLocation:       filepath.Dir(executable),
		UninstallString:       `"C:\ProgramData\PrintMaster\bin\printmaster-agent.exe" --service uninstall`,
		QuietUninstallString:  `"C:\ProgramData\PrintMaster\bin\printmaster-agent.exe" --service uninstall --silent`,
		WindowsInstallerKnown: true,
	}
	if got, matched := knownBootstrapUninstallExecutable(entry, programData); !matched || !strings.EqualFold(got, executable) {
		t.Fatalf("expected legacy bootstrap entry to match, got path=%q matched=%t", got, matched)
	}

	cases := []struct {
		name   string
		mutate func(*windowsUninstallEntry)
	}{
		{"other registry hive", func(e *windowsUninstallEntry) { e.RegistryHive = "HKCU" }},
		{"other key", func(e *windowsUninstallEntry) { e.RegistryPath += "-other" }},
		{"other publisher", func(e *windowsUninstallEntry) { e.Publisher = "Other" }},
		{"other install directory", func(e *windowsUninstallEntry) { e.InstallLocation = `C:\Other` }},
		{"Windows Installer record", func(e *windowsUninstallEntry) { e.WindowsInstaller = true }},
		{"unknown Windows Installer state", func(e *windowsUninstallEntry) { e.WindowsInstallerKnown = false }},
		{"other uninstaller executable", func(e *windowsUninstallEntry) { e.UninstallString = `"C:\Other\agent.exe" --service uninstall` }},
		{"other quiet uninstall arguments", func(e *windowsUninstallEntry) { e.QuietUninstallString += " --force" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			candidate := entry
			test.mutate(&candidate)
			if _, matched := knownBootstrapUninstallExecutable(candidate, programData); matched {
				t.Fatal("mismatched uninstall entry was accepted")
			}
		})
	}
}
