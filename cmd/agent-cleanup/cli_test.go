package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Printmaster-Org/printmaster-agent-cleanup/internal/cleanup"
)

type testBackend struct {
	inventory cleanup.Inventory
	elevated  bool
	actions   []cleanup.Action
}

func (b *testBackend) Scan(context.Context) (cleanup.Inventory, error) { return b.inventory, nil }
func (b *testBackend) IsElevated() bool                                { return b.elevated }
func (b *testBackend) Execute(_ context.Context, action cleanup.Action) error {
	b.actions = append(b.actions, action)
	return nil
}

func TestScanIsReadOnlyAndReportsUnknowns(t *testing.T) {
	backend := &testBackend{inventory: cleanup.Inventory{
		Platform:     cleanup.PlatformWindows,
		Unrecognized: []cleanup.UnrecognizedEntry{{Kind: "service", Name: "PrintMasterAgent", Reason: "unrecognized path"}},
	}}
	var output strings.Builder
	if err := runCLI([]string{"scan"}, backend, &output); err != nil {
		t.Fatal(err)
	}
	if len(backend.actions) != 0 {
		t.Fatalf("scan executed actions: %+v", backend.actions)
	}
	if !strings.Contains(output.String(), "unrecognized path") {
		t.Fatal("scan omitted unknown-entry warning")
	}
}

func TestGUIAndCLIEntryModes(t *testing.T) {
	if !shouldOpenGUI(nil, "gui") || !shouldOpenGUI([]string{"--gui"}, "gui") {
		t.Fatal("GUI default and explicit GUI mode should open the wizard")
	}
	if shouldOpenGUI(nil, "cli") || shouldOpenGUI([]string{"scan"}, "gui") {
		t.Fatal("CLI mode or an explicit CLI command must not open the GUI")
	}
}

func TestHelpTextDescribesDefaultLaunchMode(t *testing.T) {
	var output strings.Builder
	defaultMode = "gui"
	if err := runCLI([]string{"help"}, &testBackend{}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "No arguments opens the graphical cleanup wizard.") {
		t.Fatal("GUI help does not describe the default launch")
	}
	output.Reset()
	defaultMode = "cli"
	if err := runCLI([]string{"help"}, &testBackend{}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "No arguments displays this help.") {
		t.Fatal("CLI help does not describe the default launch")
	}
	defaultMode = "gui"
}

func TestRemoveRequiresYesAndElevation(t *testing.T) {
	backend := &testBackend{elevated: true, inventory: cleanup.Inventory{Platform: cleanup.PlatformLinux, Installations: []cleanup.Installation{{
		LayoutID: "direct", Method: cleanup.MethodRawBinary, Executables: []string{"/usr/local/bin/printmaster-agent"},
	}}}}
	var output strings.Builder
	if err := runCLI([]string{"remove"}, backend, &output); err == nil {
		t.Fatal("remove ran without --yes")
	}
	if len(backend.actions) != 0 {
		t.Fatal("unconfirmed remove executed actions")
	}
	if err := runCLI([]string{"remove", "--yes"}, backend, &output); err != nil {
		t.Fatal(err)
	}
	if len(backend.actions) != 1 || backend.actions[0].Kind != cleanup.ActionRemoveExecutable {
		t.Fatalf("unexpected actions: %+v", backend.actions)
	}
}

func TestPurgeRequiresExplicitSelection(t *testing.T) {
	backend := &testBackend{elevated: true, inventory: cleanup.Inventory{Platform: cleanup.PlatformWindows, Installations: []cleanup.Installation{{
		LayoutID: "direct", Method: cleanup.MethodRawBinary,
		DataPaths: []string{`C:\ProgramData\PrintMaster\agent`},
	}}}}
	var output strings.Builder
	if err := runCLI([]string{"plan"}, backend, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "purge-data C:\\ProgramData") {
		t.Fatal("default plan includes data purge")
	}
	output.Reset()
	if err := runCLI([]string{"plan", "--purge-data"}, backend, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), string(cleanup.ActionPurgeData)) {
		t.Fatal("explicit purge plan omitted data deletion")
	}
}

func TestRemoveBlocksUnknownEntries(t *testing.T) {
	backend := &testBackend{elevated: true, inventory: cleanup.Inventory{
		Platform:      cleanup.PlatformWindows,
		Installations: []cleanup.Installation{{LayoutID: "known", Method: cleanup.MethodRawBinary, Executables: []string{`C:\Program Files\PrintMaster\agent.exe`}}},
		Unrecognized:  []cleanup.UnrecognizedEntry{{Kind: "service", Name: "PrintMasterAgent", Reason: "custom ImagePath"}},
	}}
	var output strings.Builder
	err := runCLI([]string{"remove", "--yes"}, backend, &output)
	if !errors.Is(err, cleanup.ErrUnknownEntries) {
		t.Fatalf("remove error = %v", err)
	}
	if len(backend.actions) != 0 {
		t.Fatal("unknown entry did not block all actions")
	}
}
