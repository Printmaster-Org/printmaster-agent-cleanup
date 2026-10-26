package cleanup

import (
	"context"
	"fmt"
	"sort"
)

// BuildPlan produces application-removal actions and, only when requested,
// separate destructive data actions. Unknown inventory is always report-only.
func BuildPlan(inventory Inventory, purgeData bool) (Plan, error) {
	plan := Plan{Platform: inventory.Platform, Actions: make([]Action, 0), PurgeData: purgeData}
	seen := make(map[string]struct{})
	add := func(install Installation, kind ActionKind, target, reason string, destructive bool) {
		key := string(kind) + "\x00" + target
		if _, ok := seen[key]; ok || target == "" {
			return
		}
		seen[key] = struct{}{}
		plan.Actions = append(plan.Actions, Action{Kind: kind, Target: target, LayoutID: install.LayoutID,
			Method: install.Method, ServiceScope: install.ServiceScope, ServicePlist: install.ServicePlist,
			Destructive: destructive, Reason: reason})
	}
	addProcess := func(install Installation, process Process) {
		key := string(ActionStopProcess) + "\x00" + fmt.Sprint(process.PID)
		if _, ok := seen[key]; ok || process.PID == 0 {
			return
		}
		seen[key] = struct{}{}
		plan.Actions = append(plan.Actions, Action{
			Kind: ActionStopProcess, Target: fmt.Sprint(process.PID), ProcessPath: process.Path,
			LayoutID: install.LayoutID, Method: install.Method, ServiceScope: install.ServiceScope,
			ServicePlist: install.ServicePlist, Destructive: true,
			Reason: "stop the discovered Agent process before removing its files",
		})
	}

	for _, install := range inventory.Installations {
		if install.LayoutID == "" {
			return Plan{}, fmt.Errorf("recognized installation is missing layout ID")
		}
		if install.ServiceName != "" {
			add(install, ActionStopService, install.ServiceName, "stop the recognized Agent service before cleanup", true)
		}
		switch install.Method {
		case MethodMSI:
			if install.MSIProductCode == "" {
				return Plan{}, fmt.Errorf("MSI layout %q has no product code", install.LayoutID)
			}
			add(install, ActionUninstallMSI, install.MSIProductCode, "remove the matching registered MSI product", true)
		case MethodDeb, MethodRPM:
			if install.PackageName == "" {
				return Plan{}, fmt.Errorf("package layout %q has no package identity", install.LayoutID)
			}
			add(install, ActionRemovePackage, install.PackageName, "remove the package without purging configuration or data", true)
		case MethodService, MethodRawBinary, MethodInteractive:
			if install.ServiceName != "" {
				add(install, ActionRemoveService, install.ServiceName, "remove the recognized Agent service registration", true)
			}
		default:
			return Plan{}, fmt.Errorf("layout %q has unsupported install method %q", install.LayoutID, install.Method)
		}
		for _, process := range install.Processes {
			if process.PID == 0 {
				return Plan{}, fmt.Errorf("layout %q contains a process without a PID", install.LayoutID)
			}
			addProcess(install, process)
		}
		if install.Method == MethodService || install.Method == MethodRawBinary || install.Method == MethodInteractive {
			for _, executable := range install.Executables {
				add(install, ActionRemoveExecutable, executable, "remove the exact discovered Agent executable", true)
			}
		}
		if purgeData {
			for _, config := range install.ConfigPaths {
				add(install, ActionPurgeConfig, config, "explicit purge of Agent configuration", true)
			}
			for _, data := range install.DataPaths {
				add(install, ActionPurgeData, data, "explicit purge of Agent identity, database, credentials, and update trust", true)
			}
			for _, logs := range install.LogPaths {
				add(install, ActionPurgeLogs, logs, "explicit purge of Agent logs", true)
			}
		}
	}
	if len(inventory.Unrecognized) > 0 {
		plan.Warnings = append(plan.Warnings, "unrecognized entries are report-only and require owner review")
	}
	sort.SliceStable(plan.Actions, func(i, j int) bool {
		return actionOrder(plan.Actions[i].Kind) < actionOrder(plan.Actions[j].Kind)
	})
	return plan, nil
}

func actionOrder(kind ActionKind) int {
	switch kind {
	case ActionStopService:
		return 0
	case ActionStopProcess:
		return 1
	case ActionUninstallMSI, ActionRemovePackage:
		return 2
	case ActionRemoveService:
		return 3
	case ActionRemoveExecutable:
		return 4
	case ActionPurgeConfig:
		return 5
	case ActionPurgeData:
		return 6
	case ActionPurgeLogs:
		return 7
	default:
		return 100
	}
}

// ApplyPlan executes only a previously reviewed plan. Unknown inventory blocks
// mutation, and all removals require elevation plus explicit confirmation.
func ApplyPlan(ctx context.Context, backend Backend, inventory Inventory, plan Plan, confirmed bool) error {
	if backend == nil {
		return fmt.Errorf("cleanup backend is required")
	}
	if !confirmed {
		return fmt.Errorf("removal requires explicit confirmation")
	}
	if !backend.IsElevated() {
		return fmt.Errorf("removal requires administrator/root privileges")
	}
	if len(inventory.Unrecognized) > 0 {
		return fmt.Errorf("%w; no cleanup actions were executed", ErrUnknownEntries)
	}
	for index, action := range plan.Actions {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := backend.Execute(ctx, action); err != nil {
			return fmt.Errorf("action %d (%s %s) failed: %w", index+1, action.Kind, action.Target, err)
		}
	}
	return nil
}
