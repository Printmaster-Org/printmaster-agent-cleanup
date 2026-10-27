//go:build windows

package cleanup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsLeftoverRecovery(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ProgramData", root)
	bin := filepath.Join(root, "PrintMaster", "bin")
	data := filepath.Join(root, "PrintMaster", "agent")
	for _, path := range []string{bin, data} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	inventory := Inventory{Platform: PlatformWindows}
	if err := inventoryWindowsRemnants(&inventory, root); err != nil {
		t.Fatal(err)
	}
	if len(inventory.Installations) != 2 || len(inventory.Unrecognized) != 0 {
		t.Fatalf("expected recoverable data and empty directory: %+v", inventory)
	}
	plan, err := BuildPlan(inventory, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != ActionRemoveEmptyDirectory {
		t.Fatalf("data must be preserved without purge: %+v", plan)
	}
	if err := removeWindowsBootstrapDirectory(bin); err != nil {
		t.Fatal(err)
	}
	if pathExists(bin) || !pathExists(data) {
		t.Fatal("empty bin should be removed without deleting Agent data")
	}
	purge, err := BuildPlan(inventory, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(purge.Actions) != 2 || purge.Actions[0].Kind != ActionPurgeData {
		t.Fatalf("leftover data must be explicitly purgeable: %+v", purge)
	}
}

func TestWindowsBootstrapDirectoryPreservesUnknownFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ProgramData", root)
	bin := filepath.Join(root, "PrintMaster", "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(bin, "printmaster-server.exe")
	if err := os.WriteFile(file, []byte("shared server fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{Platform: PlatformWindows}
	if err := inventoryWindowsRemnants(&inventory, root); err != nil {
		t.Fatal(err)
	}
	if len(inventory.Unrecognized) != 1 || inventory.Unrecognized[0].Path != file {
		t.Fatalf("unknown/shared files must be reported: %+v", inventory)
	}
	if err := removeWindowsBootstrapDirectory(bin); err == nil {
		t.Fatal("non-empty directory removal must fail")
	}
	if !pathExists(file) {
		t.Fatal("shared server file was deleted")
	}
	if err := removeWindowsBootstrapDirectory(filepath.Dir(bin)); err == nil {
		t.Fatal("shared PrintMaster root must never be removed")
	}
}

func TestWindowsBootstrapDirectoryRemovalFollowsExecutable(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ProgramData", root)
	bin := filepath.Join(root, "PrintMaster", "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(bin, "printmaster-agent.exe")
	if err := os.WriteFile(executable, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	inventory := Inventory{Platform: PlatformWindows, Installations: []Installation{{
		LayoutID: "windows-bootstrap-bin", Method: MethodRawBinary, Executables: []string{executable},
	}}}
	if err := inventoryWindowsRemnants(&inventory, root); err != nil {
		t.Fatal(err)
	}
	if len(inventory.Installations) != 1 {
		t.Fatal("directory cleanup must stay with its owning executable selection")
	}
	plan, err := BuildPlan(inventory, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 2 || plan.Actions[1].Kind != ActionRemoveEmptyDirectory {
		t.Fatalf("directory must be removed after the executable: %+v", plan)
	}
	if err := removeWindowsBootstrapDirectory(bin); err == nil {
		t.Fatal("directory must refuse removal before its executable is gone")
	}
	if err := os.Remove(executable); err != nil {
		t.Fatal(err)
	}
	if err := removeWindowsBootstrapDirectory(bin); err != nil {
		t.Fatal(err)
	}
}
