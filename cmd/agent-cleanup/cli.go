package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Printmaster-Org/printmaster-agent-cleanup/internal/cleanup"
)

func runCLI(args []string, backend cleanup.Backend, output io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintf(output, `PrintMaster Agent Cleanup

No arguments opens the graphical cleanup wizard.

Usage:
  agent-cleanup scan [--json]
  agent-cleanup plan [--purge-data]
  agent-cleanup remove --yes [--purge-data]

Removal requires Administrator/root. Unknown entries are report-only and block cleanup.
Data purge is separate and never selected by default.`)
		return nil
	}
	command := args[0]
	flags := flag.NewFlagSet("agent-cleanup "+command, flag.ContinueOnError)
	flags.SetOutput(output)
	purgeData := flags.Bool("purge-data", false, "also remove Agent-specific configuration, identity, database, credentials, logs, and update trust")
	yes := flags.Bool("yes", false, "confirm the reviewed removal plan")
	jsonOutput := flags.Bool("json", false, "write inventory/plan as JSON")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	inventory, err := backend.Scan(context.Background())
	if err != nil {
		return fmt.Errorf("scan Agent installations: %w", err)
	}
	if command == "scan" {
		if *purgeData || *yes {
			return errors.New("scan does not accept --purge-data or --yes")
		}
		if *jsonOutput {
			return json.NewEncoder(output).Encode(inventory)
		}
		return printInventory(output, inventory)
	}
	if command != "plan" && command != "remove" {
		return fmt.Errorf("unknown command %q; use scan, plan, or remove", command)
	}
	if *jsonOutput {
		return errors.New("--json is supported only by scan")
	}
	plan, err := cleanup.BuildPlan(inventory, *purgeData)
	if err != nil {
		return err
	}
	if len(inventory.Unrecognized) > 0 {
		return cleanup.ErrUnknownEntries
	}
	if command == "plan" {
		return printPlan(output, plan)
	}
	if !*yes {
		return errors.New("remove requires --yes after reviewing `agent-cleanup plan`")
	}
	return cleanup.ApplyPlan(context.Background(), backend, inventory, plan, true)
}

func printInventory(output io.Writer, inventory cleanup.Inventory) error {
	fmt.Fprintf(output, "Platform: %s\nRecognized installations: %d\n", inventory.Platform, len(inventory.Installations))
	for _, installation := range inventory.Installations {
		fmt.Fprintf(output, "\n- %s (%s", installation.LayoutID, installation.Method)
		if installation.Version != "" {
			fmt.Fprintf(output, ", version %s", installation.Version)
		}
		fmt.Fprintln(output, ")")
		for _, path := range installation.Executables {
			fmt.Fprintf(output, "  executable: %s\n", path)
		}
		for _, evidence := range installation.Evidence {
			fmt.Fprintf(output, "  evidence: %s\n", evidence)
		}
	}
	for _, unknown := range inventory.Unrecognized {
		fmt.Fprintf(output, "\n? %s %s: %s\n  %s\n", unknown.Kind, unknown.Name, unknown.Path, unknown.Reason)
		for _, evidence := range unknown.Evidence {
			fmt.Fprintf(output, "  evidence: %s\n", evidence)
		}
	}
	return nil
}

func printPlan(output io.Writer, plan cleanup.Plan) error {
	fmt.Fprintf(output, "Removal plan for %s (%d actions)\n", plan.Platform, len(plan.Actions))
	for index, action := range plan.Actions {
		fmt.Fprintf(output, "%d. %s %s — %s\n", index+1, action.Kind, action.Target, action.Reason)
	}
	if !plan.PurgeData {
		fmt.Fprintln(output, "Agent data will be preserved. Use --purge-data only for an intentional fresh start.")
	}
	return nil
}
