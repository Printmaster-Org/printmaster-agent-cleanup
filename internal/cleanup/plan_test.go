package cleanup

import (
	"context"
	"errors"
	"testing"
)

type fakeBackend struct {
	elevated bool
	actions  []Action
}

func (b *fakeBackend) Scan(context.Context) (Inventory, error) { return Inventory{}, nil }
func (b *fakeBackend) IsElevated() bool                        { return b.elevated }
func (b *fakeBackend) Execute(_ context.Context, action Action) error {
	b.actions = append(b.actions, action)
	return nil
}

func TestKnownLayoutsCoverLegacyAndCurrentLocations(t *testing.T) {
	cases := []struct {
		platform Platform
		wantIDs  []string
	}{
		{PlatformWindows, []string{"windows-msi-protected", "windows-legacy-msi-versioned", "windows-bootstrap-bin", "windows-user-data"}},
		{PlatformLinux, []string{"linux-deb", "linux-rpm", "linux-direct-pm-agent", "linux-direct-printmaster-agent", "linux-user-data"}},
		{PlatformDarwin, []string{"darwin-service", "darwin-user-data"}},
	}
	for _, test := range cases {
		t.Run(string(test.platform), func(t *testing.T) {
			layouts := KnownLayouts(test.platform)
			if len(layouts) != len(test.wantIDs) {
				t.Fatalf("got %d layouts, want %d", len(layouts), len(test.wantIDs))
			}
			for i, want := range test.wantIDs {
				if layouts[i].ID != want {
					t.Errorf("layout %d = %q, want %q", i, layouts[i].ID, want)
				}
			}
		})
	}
	if got := KnownLayouts(Platform("plan9")); len(got) != 0 {
		t.Fatalf("unknown platform returned layouts: %+v", got)
	}
}

func TestBuildPlanPreservesDataUnlessExplicitlyRequested(t *testing.T) {
	inventory := Inventory{Platform: PlatformWindows, Installations: []Installation{{
		LayoutID: "windows-legacy", Method: MethodMSI, MSIProductCode: "{PRODUCT-CODE}",
		Executables: []string{`C:\Program Files\PrintMaster\printmaster-agent.exe`},
		ConfigPaths: []string{`C:\ProgramData\PrintMaster\agent\config.toml`},
		DataPaths:   []string{`C:\ProgramData\PrintMaster\agent`},
		LogPaths:    []string{`C:\ProgramData\PrintMaster\agent\logs`},
	}}}
	preserve, err := BuildPlan(inventory, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range preserve.Actions {
		if action.Kind == ActionPurgeConfig || action.Kind == ActionPurgeData || action.Kind == ActionPurgeLogs {
			t.Fatalf("default removal plan purges data: %+v", action)
		}
	}
	purge, err := BuildPlan(inventory, true)
	if err != nil {
		t.Fatal(err)
	}
	if purge.PurgeData == false {
		t.Fatal("purge plan did not record explicit purge selection")
	}
	for _, kind := range []ActionKind{ActionPurgeConfig, ActionPurgeData, ActionPurgeLogs} {
		found := false
		for _, action := range purge.Actions {
			found = found || action.Kind == kind
		}
		if !found {
			t.Errorf("explicit purge plan lacks %s", kind)
		}
	}
	for _, action := range purge.Actions {
		if action.Target == `C:\ProgramData\PrintMaster` {
			t.Fatal("plan must never delete the shared PrintMaster root")
		}
	}
}

func TestBuildPlanUnknownEntriesAreReportOnly(t *testing.T) {
	inventory := Inventory{
		Platform:     PlatformWindows,
		Unrecognized: []UnrecognizedEntry{{Kind: "service", Name: "PrintMasterAgent", Path: `C:\custom\agent.exe`, Reason: "path not in known layouts"}},
	}
	plan, err := BuildPlan(inventory, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("unknown entries produced destructive actions: %+v", plan.Actions)
	}
	if len(plan.Warnings) == 0 {
		t.Fatal("unknown inventory should produce an owner-review warning")
	}
}

func TestBuildPlanPackageRemovalDoesNotInvokePurge(t *testing.T) {
	inventory := Inventory{Platform: PlatformLinux, Installations: []Installation{{
		LayoutID: "linux-deb", Method: MethodDeb, PackageName: "printmaster-agent",
		DataPaths: []string{"/var/lib/printmaster/agent"}, LogPaths: []string{"/var/log/printmaster"},
	}}}
	plan, err := BuildPlan(inventory, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != ActionRemovePackage {
		t.Fatalf("unexpected package-removal plan: %+v", plan.Actions)
	}
}

func TestBuildPlanRemovesOnlyRecognizedUninstallEntry(t *testing.T) {
	inventory := Inventory{Platform: PlatformWindows, Installations: []Installation{{
		LayoutID: "windows-bootstrap-bin", Method: MethodRawBinary,
		Executables:  []string{`C:\ProgramData\PrintMaster\bin\printmaster-agent.exe`},
		UninstallKey: `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\PrintMasterAgent`,
	}}}
	plan, err := BuildPlan(inventory, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, action := range plan.Actions {
		if action.Kind == ActionRemoveUninstallEntry {
			found = action.Target == inventory.Installations[0].UninstallKey
		}
	}
	if !found {
		t.Fatalf("plan omitted the recognized legacy uninstall key: %+v", plan.Actions)
	}
}

func TestBuildPlanRejectsAmbiguousInstallOwner(t *testing.T) {
	inventory := Inventory{Platform: PlatformWindows, Installations: []Installation{{
		LayoutID: "ambiguous", Method: MethodMSI,
	}}}
	if _, err := BuildPlan(inventory, false); err == nil {
		t.Fatal("expected conflicting install ownership to fail closed")
	}
}

func TestApplyPlanRequiresConfirmationAndElevation(t *testing.T) {
	plan := Plan{Actions: []Action{{Kind: ActionRemoveExecutable, Target: "/usr/bin/printmaster-agent"}}}
	backend := &fakeBackend{elevated: true}
	if err := ApplyPlan(context.Background(), backend, Inventory{}, plan, false); err == nil {
		t.Fatal("unconfirmed removal was allowed")
	}
	if len(backend.actions) != 0 {
		t.Fatal("unconfirmed plan executed actions")
	}
	backend.elevated = false
	if err := ApplyPlan(context.Background(), backend, Inventory{}, plan, true); err == nil {
		t.Fatal("non-elevated removal was allowed")
	}
	if len(backend.actions) != 0 {
		t.Fatal("non-elevated plan executed actions")
	}
}

func TestApplyPlanBlocksUnknownInventory(t *testing.T) {
	backend := &fakeBackend{elevated: true}
	plan := Plan{Actions: []Action{{Kind: ActionRemoveExecutable, Target: "/usr/bin/printmaster-agent"}}}
	inventory := Inventory{Unrecognized: []UnrecognizedEntry{{Kind: "service", Name: "PrintMasterAgent", Reason: "unknown path"}}}
	err := ApplyPlan(context.Background(), backend, inventory, plan, true)
	if err == nil || !errors.Is(err, ErrUnknownEntries) {
		t.Fatalf("unknown inventory error = %v", err)
	}
	if len(backend.actions) != 0 {
		t.Fatal("plan executed despite unknown inventory")
	}
}
