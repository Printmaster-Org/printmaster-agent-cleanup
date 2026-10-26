package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/Printmaster-Org/printmaster-agent-cleanup/internal/cleanup"
)

type installChoice struct {
	index int
	check *widget.Check
}

func BuildWindow(app fyne.App, backend cleanup.Backend) fyne.Window {
	window := app.NewWindow("PrintMaster Agent Cleanup")
	window.Resize(fyne.NewSize(900, 760))

	heading := widget.NewLabelWithStyle("PrintMaster Agent Cleanup", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	subtitle := widget.NewLabel("Scan first. Nothing is changed until you review and confirm a removal plan.")
	status := widget.NewLabel("Preparing inventory…")

	installList := container.NewVBox()
	unknownList := container.NewVBox()
	results := widget.NewMultiLineEntry()
	results.SetPlaceHolder("Detailed inventory evidence appears here.")
	results.Disable()
	dataCheck := widget.NewCheck("Also purge Agent configuration, identity, database, credentials, logs, and update trust", nil)
	dataCheck.SetChecked(false)
	dataCheck.Disable()
	removeButton := widget.NewButton("Remove selected Agent installs", nil)
	removeButton.Disable()
	scanButton := widget.NewButton("Scan this device", nil)
	choices := make([]installChoice, 0)
	var current cleanup.Inventory
	var scanning, removing bool

	updateRemoveEnabled := func() {
		selected := false
		for _, choice := range choices {
			selected = selected || choice.check.Checked
		}
		removeButton.Disable()
		if selected && len(current.Unrecognized) == 0 && backend.IsElevated() && !scanning && !removing {
			removeButton.Enable()
		}
	}

	var scan func()
	scan = func() {
		if scanning || removing {
			return
		}
		scanning = true
		status.SetText("Scanning known Agent installations…")
		scanButton.Disable()
		removeButton.Disable()
		dataCheck.Disable()
		go func() {
			inventory, err := backend.Scan(context.Background())
			fyne.Do(func() {
				scanning = false
				installList.Objects = nil
				unknownList.Objects = nil
				choices = nil
				if err != nil {
					status.SetText("Scan failed")
					results.SetText(err.Error())
					scanButton.Enable()
					return
				}
				current = inventory
				if len(inventory.Installations) == 0 {
					installList.Add(widget.NewLabel("No known Agent installations found."))
				}
				for index, installation := range inventory.Installations {
					label := fmt.Sprintf("%s · %s", installation.LayoutID, installation.Method)
					if installation.Version != "" {
						label += " · " + installation.Version
					}
					check := widget.NewCheck(label, func(bool) { updateRemoveEnabled() })
					check.SetChecked(true)
					paths := append(append(append([]string{}, installation.Executables...), installation.ConfigPaths...), installation.DataPaths...)
					paths = append(paths, installation.LogPaths...)
					if len(installation.Processes) > 0 {
						for _, process := range installation.Processes {
							paths = append(paths, fmt.Sprintf("process %d · %s", process.PID, process.Path))
						}
					}
					installList.Add(container.NewVBox(check, widget.NewLabel(strings.Join(paths, "\n")), widget.NewLabel(strings.Join(installation.Evidence, "\n"))))
					choices = append(choices, installChoice{index: index, check: check})
				}
				for _, entry := range inventory.Unrecognized {
					unknownList.Add(widget.NewLabel(fmt.Sprintf("%s · %s · %s", entry.Kind, entry.Name, entry.Reason)))
				}
				if len(inventory.Unrecognized) > 0 {
					status.SetText("Review required · unknown entries block removal")
				} else if !backend.IsElevated() {
					status.SetText("Scan complete · reopen as Administrator/root to remove")
				} else {
					status.SetText(fmt.Sprintf("Scan complete · %d recognized installation(s)", len(inventory.Installations)))
					if len(inventory.Installations) > 0 {
						dataCheck.Enable()
					}
				}
				dataCheck.SetChecked(false)
				results.SetText(inventoryJSON(inventory))
				scanButton.Enable()
				installList.Refresh()
				unknownList.Refresh()
				updateRemoveEnabled()
			})
		}()
	}

	scanButton.OnTapped = scan
	removeButton.OnTapped = func() {
		selected := cleanup.Inventory{Platform: current.Platform, Installations: make([]cleanup.Installation, 0)}
		for _, choice := range choices {
			if choice.check.Checked {
				selected.Installations = append(selected.Installations, current.Installations[choice.index])
			}
		}
		selected.Unrecognized = append(selected.Unrecognized, current.Unrecognized...)
		plan, err := cleanup.BuildPlan(selected, dataCheck.Checked)
		if err != nil {
			dialog.ShowError(err, window)
			return
		}
		if len(plan.Actions) == 0 {
			status.SetText("Nothing selected for removal")
			return
		}
		var summary strings.Builder
		for _, action := range plan.Actions {
			fmt.Fprintf(&summary, "• %s: %s\n", action.Kind, action.Target)
		}
		if plan.PurgeData {
			summary.WriteString("\nAgent data purge is included and cannot be undone.\n")
		} else {
			summary.WriteString("\nAgent data will be preserved.\n")
		}
		confirm := func() {
			dialog.ShowConfirm("Confirm Agent removal", summary.String(), func(confirmed bool) {
				if !confirmed {
					return
				}
				if !backend.IsElevated() {
					status.SetText("Removal requires administrator/root privileges")
					return
				}
				removing = true
				status.SetText("Removal in progress…")
				scanButton.Disable()
				removeButton.Disable()
				go func() {
					err := cleanup.ApplyPlan(context.Background(), backend, selected, plan, true)
					fyne.Do(func() {
						removing = false
						if err != nil {
							status.SetText("Removal stopped; review error and rescan")
							results.SetText(err.Error())
						} else {
							status.SetText("Removal completed. Rescan before reinstalling.")
							results.SetText("Removal completed. Data policy was honored. Rescan to verify what remains.")
						}
						scanButton.Enable()
						removeButton.Disable()
					})
				}()
			}, window)
		}
		if plan.PurgeData {
			showPurgeConfirmation(window, confirm)
		} else {
			confirm()
		}
	}

	copyCommand := widget.NewButton("Copy CLI scan command", func() {
		command := "agent-cleanup scan --json"
		app.Clipboard().SetContent(command)
		status.SetText("Copied: " + command)
	})

	content := container.NewVBox(
		heading,
		subtitle,
		container.NewHBox(scanButton, copyCommand),
		status,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Recognized installations", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		installList,
		dataCheck,
		widget.NewLabelWithStyle("Unrecognized entries (report-only)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		unknownList,
		removeButton,
		widget.NewLabelWithStyle("Inventory details", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		results,
	)
	window.SetContent(container.NewScroll(content))
	scan()
	return window
}

func inventoryJSON(inventory cleanup.Inventory) string {
	data, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(data)
}

func showPurgeConfirmation(parent fyne.Window, onConfirmed func()) {
	entry := widget.NewEntry()
	entry.SetPlaceHolder("PURGE PRINTMASTER AGENT DATA")
	form := dialog.NewForm("Confirm permanent data purge", "Review purge", "Cancel",
		[]*widget.FormItem{widget.NewFormItem("Type the exact phrase to continue", entry)},
		func(confirmed bool) {
			if !confirmed {
				return
			}
			if entry.Text != "PURGE PRINTMASTER AGENT DATA" {
				dialog.ShowError(fmt.Errorf("confirmation phrase did not match; no data was purged"), parent)
				return
			}
			onConfirmed()
		}, parent)
	form.Show()
}
