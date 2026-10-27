package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/Printmaster-Org/printmaster-agent-cleanup/internal/cleanup"
)

type installChoice struct {
	index int
	check *widget.Check
}

func wrappedLabel(text string) *widget.Label {
	label := widget.NewLabel(text)
	label.Wrapping = fyne.TextWrapWord
	return label
}

func resourceLink(app fyne.App, window fyne.Window, label string, target *url.URL) *widget.Hyperlink {
	link := widget.NewHyperlink(label, target)
	link.OnTapped = func() {
		if err := app.OpenURL(target); err != nil {
			dialog.ShowError(fmt.Errorf("open %s: %w", target, err), window)
		}
	}
	return link
}

func BuildWindow(app fyne.App, backend cleanup.Backend) fyne.Window {
	app.Settings().SetTheme(cleanupTheme{Theme: theme.DefaultTheme()})
	app.SetIcon(appIcon)
	window := app.NewWindow("PrintMaster Agent Cleanup")
	window.SetIcon(appIcon)
	window.Resize(fyne.NewSize(960, 820))

	heading := widget.NewRichText(&widget.TextSegment{
		Text: "Agent cleanup", Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{Bold: true}, SizeName: theme.SizeNameHeadingText},
	})
	brand := widget.NewLabel("PRINTMASTER  /  RECOVERY TOOLS")
	brand.Importance = widget.LowImportance
	logo := canvas.NewImageFromResource(appIcon)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(44, 44))
	pageTitle := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	stepLabels := []*widget.Label{
		widget.NewLabel("01  Scan"), widget.NewLabel("02  Select"),
		widget.NewLabel("03  Review"), widget.NewLabel("04  Done"),
	}
	stepObjects := make([]fyne.CanvasObject, len(stepLabels))
	for i, label := range stepLabels {
		stepObjects[i] = label
	}
	status := wrappedLabel("")
	body := container.NewStack()
	actions := container.NewStack()
	window.SetContent(inset(container.NewBorder(
		container.NewVBox(
			container.NewBorder(nil, nil, container.NewHBox(logo), brand, heading),
			container.NewGridWithColumns(4, stepObjects...), widget.NewSeparator(), pageTitle, status,
		),
		container.NewVBox(widget.NewSeparator(), inset(actions, 6)), nil, nil, body,
	), 20))

	var current cleanup.Inventory
	var selected cleanup.Inventory
	var plan cleanup.Plan
	var choices []installChoice
	var scanning, removing, reviewing bool
	var scan, showSelection, showReview, showStart func()
	var resultsText string

	setPage := func(step string, content fyne.CanvasObject, buttons ...fyne.CanvasObject) {
		for i, label := range stepLabels {
			label.Importance = widget.LowImportance
			label.TextStyle.Bold = false
			if strings.HasPrefix(step, fmt.Sprint(i+1)+".") {
				label.Importance = widget.HighImportance
				label.TextStyle.Bold = true
			}
			label.Refresh()
		}
		body.Objects = []fyne.CanvasObject{content}
		if len(buttons) == 1 {
			actions.Objects = []fyne.CanvasObject{container.NewHBox(layout.NewSpacer(), buttons[0])}
		} else {
			actions.Objects = []fyne.CanvasObject{container.NewBorder(nil, nil,
				container.NewHBox(buttons[:len(buttons)-1]...), buttons[len(buttons)-1], nil)}
		}
		body.Refresh()
		actions.Refresh()
		pageTitle.SetText(step)
	}

	showResults := func(message, details string) {
		resultsText = details
		restart := widget.NewButton("Return to start", func() { showStart() })
		restart.Importance = widget.HighImportance
		copyReport := widget.NewButton("Copy result", func() {
			app.Clipboard().SetContent(resultsText)
		})
		links := container.NewHBox()
		for _, resource := range []struct{ label, host, path string }{
			{"PrintMaster website", "printmaster.work", ""},
			{"Documentation", "docs.printmaster.work", ""},
			{"Support / GitHub", "github.com", "/Printmaster-Org/printmaster"},
		} {
			target := &url.URL{Scheme: "https", Host: resource.host, Path: resource.path}
			links.Add(resourceLink(app, window, resource.label, target))
		}
		setPage("4. Done", container.NewVScroll(container.NewVBox(
			section(message, details),
			section("What's next?", "Visit PrintMaster for installation guidance and help.", links),
			wrappedLabel("Return to start for another scan to verify what remains, or close this window when finished."),
		)), copyReport, restart)
	}

	apply := func() {
		if removing || scanning || !reviewing {
			return
		}
		if !backend.IsElevated() {
			dialog.ShowError(fmt.Errorf("removal requires administrator/root privileges"), window)
			return
		}
		removing = true
		reviewing = false
		status.SetText("Removal in progress. Do not close this window.")
		progress := widget.NewProgressBarInfinite()
		progress.Start()
		wait := widget.NewButton("Removing...", nil)
		wait.Disable()
		setPage("3. Review · Removing", container.NewVBox(
			wrappedLabel("Applying the reviewed removal plan..."), progress,
		), wait)
		go func() {
			err := cleanup.ApplyPlan(context.Background(), backend, selected, plan, true)
			fyne.Do(func() {
				progress.Stop()
				removing = false
				if err != nil {
					status.SetText("Removal stopped. Review the error and rescan.")
					showResults("Cleanup did not complete", err.Error())
					return
				}
				status.SetText("Removal completed.")
				showResults("Cleanup completed", "Removal completed. The selected data policy was honored.")
			})
		}()
	}

	showReview = func() {
		selected = cleanup.Inventory{Platform: current.Platform, Unrecognized: current.Unrecognized}
		for _, choice := range choices {
			if choice.check.Checked {
				selected.Installations = append(selected.Installations, current.Installations[choice.index])
			}
		}
		var err error
		plan, err = cleanup.BuildPlan(selected, plan.PurgeData)
		if err != nil {
			dialog.ShowError(err, window)
			return
		}
		if len(plan.Actions) == 0 {
			dialog.ShowInformation("Nothing to remove", "Select an installation with removable components.", window)
			return
		}
		reviewing = true
		policy := "Your Agent data will be preserved, including configuration, identity, credentials, and history."
		if plan.PurgeData {
			policy = "PERMANENT DATA PURGE: Configuration, identity, databases, credentials, logs, and update trust will be deleted. This cannot be undone."
		}
		actionList := container.NewVBox()
		counts := make(map[cleanup.ActionKind]int)
		var kinds []cleanup.ActionKind
		for _, action := range plan.Actions {
			if counts[action.Kind] == 0 {
				kinds = append(kinds, action.Kind)
			}
			counts[action.Kind]++
			actionList.Add(container.NewVBox(
				widget.NewLabelWithStyle(strings.ReplaceAll(string(action.Kind), "-", " "), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
				wrappedLabel(action.Target), wrappedLabel(action.Reason), widget.NewSeparator(),
			))
		}
		var summary []string
		for _, kind := range kinds {
			summary = append(summary, fmt.Sprintf("%d × %s", counts[kind], strings.ReplaceAll(string(kind), "-", " ")))
		}
		remove := newHoldButton(apply)
		back := widget.NewButton("Back to selection", func() {
			remove.cancel()
			showSelection()
		})
		status.SetText("Ready to remove. Nothing has been changed yet.")
		setPage("3. Review", container.NewVScroll(container.NewVBox(
			section("Confirm removal",
				fmt.Sprintf("%d installation(s) selected · %d planned action(s)", len(selected.Installations), len(plan.Actions)),
				wrappedLabel(strings.Join(summary, "  ·  ")),
			),
			section("Data policy", policy),
			wrappedLabel("Hold for 3 seconds. Removal starts when the button fills. Release early to cancel. Keyboard: hold Space."),
			widget.NewAccordion(widget.NewAccordionItem("View full removal details", actionList)),
		)), back, remove)
	}

	showSelection = func() {
		reviewing = false
		installList := container.NewVBox()
		unknownList := container.NewVBox()
		review := widget.NewButton("Review removal plan", func() { showReview() })
		review.Importance = widget.HighImportance
		updateReview := func() {
			hasSelection := false
			for _, choice := range choices {
				hasSelection = hasSelection || choice.check.Checked
			}
			review.Disable()
			if hasSelection && len(current.Unrecognized) == 0 && backend.IsElevated() {
				review.Enable()
			}
		}
		if choices == nil {
			for index, installation := range current.Installations {
				label := "Remove this installation"
				if installation.Version != "" {
					label += " · " + installation.Version
				}
				check := widget.NewCheck(label, func(bool) { updateReview() })
				check.SetChecked(true)
				choices = append(choices, installChoice{index: index, check: check})
			}
		}
		for _, choice := range choices {
			choice.check.OnChanged = func(bool) { updateReview() }
			installation := current.Installations[choice.index]
			paths := append(append(append([]string{}, installation.Executables...), installation.ConfigPaths...), installation.DataPaths...)
			paths = append(paths, installation.LogPaths...)
			paths = append(paths, installation.EmptyDirectories...)
			for _, process := range installation.Processes {
				paths = append(paths, fmt.Sprintf("process %d · %s", process.PID, process.Path))
			}
			details := container.NewVBox(
				wrappedLabel("Layout: "+installation.LayoutID),
				wrappedLabel(strings.Join(paths, "\n")),
				wrappedLabel(strings.Join(installation.Evidence, "\n")),
			)
			summary := fmt.Sprintf("Installation type: %s", installation.Method)
			if installation.Version != "" {
				summary += "  ·  Version: " + installation.Version
			}
			if installation.ServiceName != "" {
				summary += "  ·  Service: " + installation.ServiceName
			}
			title := "PrintMaster Agent"
			if len(installation.Executables) == 0 && installation.ServiceName == "" && installation.Method == cleanup.MethodInteractive {
				title = "Leftover Agent files / directories"
			}
			installList.Add(section(title, summary,
				choice.check,
				widget.NewAccordion(widget.NewAccordionItem("Paths and identification evidence", details)),
			))
		}
		if len(current.Installations) == 0 {
			installList.Add(wrappedLabel("No known Agent installations found."))
		}
		for _, entry := range current.Unrecognized {
			details := append([]string{entry.Reason, entry.Path}, entry.Evidence...)
			unknownList.Add(section(entry.Kind+" · "+entry.Name, "", wrappedLabel(strings.Join(details, "\n"))))
		}
		if len(current.Unrecognized) > 0 {
			status.SetText("Review required: unrecognized entries block all removal.")
		} else if !backend.IsElevated() {
			status.SetText("Read-only scan. Reopen as Administrator/root to remove installations.")
		} else {
			status.SetText(fmt.Sprintf("Scan complete · %d recognized installation(s)", len(current.Installations)))
		}
		dataCheck := widget.NewCheck("Permanently purge Agent data", func(checked bool) { plan.PurgeData = checked })
		dataCheck.SetChecked(plan.PurgeData)
		if len(current.Installations) == 0 || len(current.Unrecognized) > 0 || !backend.IsElevated() {
			dataCheck.Disable()
		}
		content := container.NewVBox(
			installList,
			section("Keep your data", "Agent data is preserved by default. Purge permanently deletes configuration, identity, databases, credentials, logs, and update trust.",
				dataCheck,
			),
		)
		if len(current.Unrecognized) > 0 {
			content.Add(widget.NewLabelWithStyle("Unrecognized entries · removal blocked", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
			content.Add(unknownList)
		}
		copyCommand := widget.NewButton("Copy CLI scan command", func() { app.Clipboard().SetContent("agent-cleanup scan --json") })
		copyCommand.Importance = widget.LowImportance
		content.Add(widget.NewAccordion(widget.NewAccordionItem("Advanced diagnostics", container.NewVBox(
			copyCommand, wrappedLabel(inventoryJSON(current)),
		))))
		updateReview()
		setPage("2. Select", container.NewVScroll(content),
			widget.NewButton("Scan again", func() { scan() }),
			review)
	}

	scan = func() {
		if scanning || removing {
			return
		}
		scanning = true
		reviewing = false
		choices = nil
		plan = cleanup.Plan{}
		status.SetText("Scanning known Agent installations...")
		progress := widget.NewProgressBarInfinite()
		progress.Start()
		wait := widget.NewButton("Scanning...", nil)
		wait.Disable()
		setPage("1. Scan", container.NewVBox(
			wrappedLabel("Checking known Agent installations, services, processes, and data locations. Nothing is being changed."),
			progress,
		), wait)
		go func() {
			inventory, err := backend.Scan(context.Background())
			fyne.Do(func() {
				progress.Stop()
				scanning = false
				if err != nil {
					status.SetText("Scan failed. No cleanup actions were performed.")
					showResults("Unable to scan this device", err.Error())
					return
				}
				current = inventory
				showSelection()
			})
		}()
	}

	showStart = func() {
		current = cleanup.Inventory{}
		selected = cleanup.Inventory{}
		plan = cleanup.Plan{}
		choices = nil
		reviewing = false
		resultsText = ""
		start := widget.NewButtonWithIcon("Scan this device", theme.SearchIcon(), func() { scan() })
		start.Importance = widget.HighImportance
		guide := resourceLink(app, window, "Read the cleanup utility guide",
			&url.URL{Scheme: "https", Host: "docs.printmaster.work", Path: "/deployment/agent-cleanup/"})
		status.SetText("Ready when you are. Start with a read-only scan.")
		setPage("1. Scan", container.NewVScroll(container.NewVBox(
			section("A clean start for your Agent", "Find and remove known PrintMaster Agent installations without guessing which files belong to them.",
				wrappedLabel("01  Scan for known installations and services."),
				wrappedLabel("02  Select installations and choose whether to keep their data."),
				wrappedLabel("03  Review the plan, then hold to confirm removal."),
			),
			section("You are in control", "Opening this tool does not scan or change your device. Unknown installations block removal, and Agent data is preserved by default."),
		)), guide, start)
	}
	showStart()
	return window
}

func inventoryJSON(inventory cleanup.Inventory) string {
	data, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(data)
}
