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
	window := app.NewWindow("PrintMaster Agent Cleanup Tool")
	window.SetIcon(appIcon)
	window.Resize(fyne.NewSize(960, 820))

	heading := widget.NewRichText(&widget.TextSegment{
		Text: "PrintMaster Agent Cleanup Tool", Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{Bold: true}, SizeName: theme.SizeNameHeadingText},
	})
	heading.Wrapping = fyne.TextWrapWord
	logo := canvas.NewImageFromResource(appIcon)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(44, 44))
	pageTitle := widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	steps := []*stepIndicator{
		newStepIndicator("01  Start"), newStepIndicator("02  Select"),
		newStepIndicator("03  Review"), newStepIndicator("04  Done"),
	}
	stepObjects := make([]fyne.CanvasObject, len(steps))
	for i, step := range steps {
		stepObjects[i] = step
	}
	status := wrappedLabel("")
	body := container.NewStack()
	actions := container.NewStack()
	actionSeparator := widget.NewSeparator()
	window.SetContent(inset(container.NewBorder(
		container.NewVBox(
			container.NewBorder(nil, nil, container.NewHBox(logo), nil, heading),
			container.NewGridWithColumns(4, stepObjects...), pageTitle, status,
		),
		container.NewVBox(actionSeparator, inset(actions, 6)), nil, nil, body,
	), 20))

	var current cleanup.Inventory
	var selected cleanup.Inventory
	var plan cleanup.Plan
	var choices []installChoice
	var scanning, removing, reviewing bool
	var scan, showSelection, showReview, showStart func()
	var resultsText string

	setPage := func(step string, content fyne.CanvasObject, buttons ...fyne.CanvasObject) {
		for i, indicator := range steps {
			indicator.active = strings.HasPrefix(step, fmt.Sprint(i+1)+".")
			indicator.Refresh()
		}
		body.Objects = []fyne.CanvasObject{content}
		if len(buttons) == 0 {
			actionSeparator.Hide()
			actions.Objects = []fyne.CanvasObject{widget.NewLabel("")}
		} else if len(buttons) == 1 {
			actionSeparator.Show()
			actions.Objects = []fyne.CanvasObject{container.NewHBox(layout.NewSpacer(), buttons[0])}
		} else {
			actionSeparator.Show()
			actions.Objects = []fyne.CanvasObject{container.NewBorder(nil, nil,
				container.NewHBox(buttons[:len(buttons)-1]...), buttons[len(buttons)-1], nil)}
		}
		body.Refresh()
		actions.Refresh()
		pageTitle.SetText(step)
	}

	showResults := func(message, details string, success bool) {
		resultsText = message + "\n" + details
		if current.Platform != "" {
			resultsText += "\n\nScan inventory (before any cleanup):\n" + inventoryJSON(current)
		}
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
		summary := container.NewVBox()
		if success {
			check := canvas.NewImageFromResource(theme.NewSuccessThemedResource(theme.ConfirmIcon()))
			check.FillMode = canvas.ImageFillContain
			check.SetMinSize(fyne.NewSize(80, 80))
			summary.Add(container.NewCenter(check))
		}
		title := widget.NewRichText(&widget.TextSegment{
			Text: message, Style: widget.RichTextStyle{
				Alignment: fyne.TextAlignCenter, TextStyle: fyne.TextStyle{Bold: true}, SizeName: theme.SizeNameHeadingText,
			},
		})
		title.Wrapping = fyne.TextWrapWord
		summary.Add(title)
		description := wrappedLabel(details)
		description.Alignment = fyne.TextAlignCenter
		summary.Add(description)
		setPage("4. Done", container.NewVScroll(container.NewVBox(
			widget.NewCard("", "", inset(summary, 12)),
			container.NewCenter(links),
			widget.NewAccordion(widget.NewAccordionItem("Scan / result details", wrappedLabel(resultsText))),
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
					showResults("Cleanup did not complete", err.Error(), false)
					return
				}
				status.SetText("Removal completed.")
				details := "The selected Agent components were removed. Your Agent data was preserved."
				if plan.PurgeData {
					details = "The selected Agent components and their selected data were removed."
				}
				showResults("All cleaned up", details, true)
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
		if len(current.Installations) > 1 {
			installList = container.NewGridWithColumns(2)
		}
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
		dataPolicy := section("Keep your data", "Data is preserved by default. Purge permanently deletes Agent configuration, identity, databases, credentials, logs, and update trust.", dataCheck)
		content := container.NewVBox(installList)
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
		setPage("2. Select", container.NewBorder(nil, dataPolicy, nil, nil, container.NewVScroll(content)),
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
		setPage("1. Start · Scanning", container.NewVBox(
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
					showResults("Unable to scan this device", err.Error(), false)
					return
				}
				current = inventory
				if len(current.Installations) == 0 && len(current.Unrecognized) == 0 {
					status.SetText("Scan complete. No changes were made.")
					showResults("Nothing to clean up", "No known PrintMaster Agent installations or leftover files were detected.", true)
					return
				}
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
		status.SetText("Ready when you are. Nothing has been changed.")
		description := wrappedLabel("Find known Agent installations and leftover files. Scanning is read-only; you review every removal.")
		description.Alignment = fyne.TextAlignCenter
		policy := wrappedLabel("Your Agent data is kept unless you explicitly choose to purge it.")
		policy.Alignment = fyne.TextAlignCenter
		splash := container.NewVBox(
			section("A clean start for your Agent", "", description, policy,
				inset(container.NewCenter(start), 12)),
			container.NewCenter(guide),
		)
		setPage("1. Start", container.NewVScroll(container.New(verticalCenterLayout{}, splash)))
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
