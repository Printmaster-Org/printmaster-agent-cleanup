//go:build windows

package cleanup

import (
	"path/filepath"
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
