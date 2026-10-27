package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/Printmaster-Org/printmaster-agent-cleanup/internal/cleanup"
)

func waitForPage(t *testing.T, window fyne.Window, text string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		found := false
		fyne.DoAndWait(func() {
			found = strings.Contains(test.RenderToMarkup(window.Canvas()), text)
		})
		if found {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("wizard did not show %q", text)
}

func tapWizardButton(t *testing.T, root fyne.CanvasObject, text string) {
	t.Helper()
	for _, object := range renderedObjects(root) {
		if button, ok := object.(*widget.Button); ok && button.Text == text {
			if button.Disabled() {
				t.Fatalf("button %q is disabled", text)
			}
			test.Tap(button)
			return
		}
	}
	t.Fatalf("button %q not found", text)
}

func wizardBackend() *uiTestBackend {
	return &uiTestBackend{
		inventory: cleanup.Inventory{
			Platform: cleanup.PlatformWindows,
			Installations: []cleanup.Installation{{
				LayoutID: "windows-legacy", Method: cleanup.MethodMSI, MSIProductCode: "{fixture}",
				DataPaths: []string{`C:\ProgramData\PrintMaster\agent`},
			}},
		},
		scanned: make(chan struct{}),
	}
}

func assertCompletionCheck(t *testing.T, window fyne.Window, want bool) {
	t.Helper()
	found := false
	var checkCenter float32
	for _, object := range renderedObjects(window.Content()) {
		if image, ok := object.(*canvas.Image); ok && image.Resource != nil &&
			image.Resource.Name() == theme.NewSuccessThemedResource(theme.ConfirmIcon()).Name() {
			found = true
			checkCenter = fyne.CurrentApp().Driver().AbsolutePositionForObject(image).X + image.Size().Width/2
			if image.MinSize().Width < 80 || image.MinSize().Height < 80 {
				t.Fatal("completion check should be prominent")
			}
		}
	}
	if found != want {
		t.Fatalf("success check visible=%t, want %t", found, want)
	}
	if !want {
		return
	}
	foundTitle := false
	for _, object := range renderedObjects(window.Content()) {
		if title, ok := object.(*widget.RichText); ok && (title.String() == "Nothing to clean up" || title.String() == "All cleaned up") {
			foundTitle = true
			for _, child := range renderedObjects(title) {
				if text, ok := child.(*canvas.Text); ok && text.Text == title.String() {
					if text.Alignment != fyne.TextAlignCenter {
						t.Fatal("completion heading glyphs must be center-aligned")
					}
					center := fyne.CurrentApp().Driver().AbsolutePositionForObject(title).X + text.Position().X + text.Size().Width/2
					offset := center - checkCenter
					if offset < -1 || offset > 1 {
						t.Fatalf("completion heading is not centered beneath check: heading=%g check=%g", center, checkCenter)
					}
				}
			}
		}
	}
	if !foundTitle {
		t.Fatal("rendered completion heading not found")
	}
}

func TestWizardCleanScanFinishesWithoutRemoval(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("read_only_%t", readOnly), func(t *testing.T) {
			application := test.NewTempApp(t)
			backend := wizardBackend()
			backend.inventory.Installations = nil
			backend.readOnly = readOnly
			window := BuildWindow(application, backend)
			t.Cleanup(window.Close)
			window.Resize(fyne.NewSize(900, 760))
			window.Show()
			tapWizardButton(t, window.Content(), "Scan this device")
			waitForPage(t, window, "4. Done")
			waitForPage(t, window, "Nothing to clean up")
			assertCompletionCheck(t, window, true)
			assertPageFits(t, window)
			for _, object := range renderedObjects(window.Content()) {
				if _, ok := object.(*widget.Check); ok {
					t.Fatal("clean scan must not offer removal or purge selections")
				}
				if accordion, ok := object.(*widget.Accordion); ok && accordion.Items[0].Open {
					t.Fatal("scan details should be collapsed")
				}
			}
			tapWizardButton(t, window.Content(), "Copy result")
			if !strings.Contains(application.Clipboard().Content(), "Scan inventory") {
				t.Fatal("clean result must retain scan diagnostics")
			}
			tapWizardButton(t, window.Content(), "Return to start")
			waitForPage(t, window, "1. Start")
			if backend.scanCalls != 1 || len(backend.actions) != 0 {
				t.Fatal("clean scan or returning to start mutated the host or rescanned")
			}
		})
	}
}

func TestWizardDoesNotTreatUnknownsOrLeftoversAsClean(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprintf("unknown_%t", unknown), func(t *testing.T) {
			application := test.NewTempApp(t)
			backend := wizardBackend()
			backend.inventory.Installations = nil
			if unknown {
				backend.inventory.Unrecognized = []cleanup.UnrecognizedEntry{{Kind: "bootstrap-remnant", Name: "unknown", Reason: "owner review required"}}
			} else {
				backend.inventory.Installations = []cleanup.Installation{{
					LayoutID: "windows-machine-data", Method: cleanup.MethodInteractive,
					DataPaths: []string{`C:\ProgramData\PrintMaster\agent`},
				}}
			}
			window := BuildWindow(application, backend)
			t.Cleanup(window.Close)
			window.Show()
			tapWizardButton(t, window.Content(), "Scan this device")
			waitForPage(t, window, "2. Select")
			assertCompletionCheck(t, window, false)
			if len(backend.actions) != 0 {
				t.Fatal("scan changed the device")
			}
		})
	}
}

func TestWizardStartsAtWelcomeWithoutScanning(t *testing.T) {
	application := test.NewTempApp(t)
	backend := wizardBackend()
	window := BuildWindow(application, backend)
	t.Cleanup(window.Close)
	window.Show()
	waitForPage(t, window, "1. Start")
	hasLogo := false
	hasGuide := false
	for _, object := range renderedObjects(window.Content()) {
		if image, ok := object.(*canvas.Image); ok && image.Resource == appIcon {
			hasLogo = true
		}
		if link, ok := object.(*widget.Hyperlink); ok && link.URL.String() == "https://docs.printmaster.work/deployment/agent-cleanup/" {
			hasGuide = true
		}
	}
	if !hasLogo {
		t.Fatal("wizard header must display the PrintMaster logo")
	}
	if !hasGuide {
		t.Fatal("welcome screen must link to the cleanup utility guide")
	}
	select {
	case <-backend.scanned:
		t.Fatal("opening the wizard must not scan before the user starts it")
	default:
	}
	if len(backend.actions) != 0 {
		t.Fatal("welcome screen mutated the device")
	}
	tapWizardButton(t, window.Content(), "Scan this device")
	waitForPage(t, window, "2. Select")
	select {
	case <-backend.scanned:
	default:
		t.Fatal("scan button did not scan the device")
	}
}

func TestWizardReviewAndResults(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		purge bool
		err   error
	}{
		{name: "preserve"},
		{name: "purge", purge: true},
		{name: "execution error", err: errors.New("fixture removal failure")},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			application := test.NewTempApp(t)
			backend := wizardBackend()
			backend.executeError = scenario.err
			window := BuildWindow(application, backend)
			t.Cleanup(window.Close)
			window.Show()
			tapWizardButton(t, window.Content(), "Scan this device")
			waitForPage(t, window, "2. Select")
			if scenario.purge {
				for _, object := range renderedObjects(window.Content()) {
					if check, ok := object.(*widget.Check); ok && check.Text == "Permanently purge Agent data" {
						check.SetChecked(true)
					}
				}
			}
			tapWizardButton(t, window.Content(), "Review removal plan")
			waitForPage(t, window, "3. Review")
			if len(backend.actions) != 0 {
				t.Fatal("reviewing a plan mutated the device")
			}
			policy := "will be preserved"
			if scenario.purge {
				policy = "PERMANENT DATA PURGE"
			}
			waitForPage(t, window, policy)
			tapWizardButton(t, window.Content(), "Back to selection")
			waitForPage(t, window, "2. Select")
			tapWizardButton(t, window.Content(), "Review removal plan")
			waitForPage(t, window, policy)
			var button *holdButton
			for _, object := range renderedObjects(window.Content()) {
				if hold, ok := object.(*holdButton); ok {
					button = hold
				}
			}
			if button == nil {
				t.Fatal("review must provide the hold-to-remove action")
			}
			primary := &desktop.MouseEvent{Button: desktop.MouseButtonPrimary}
			test.Tap(button)
			button.MouseDown(primary)
			button.MouseUp(primary)
			if len(backend.actions) != 0 {
				t.Fatal("clicking or releasing early mutated the device")
			}
			button.MouseDown(primary)
			button.started = time.Now().Add(-purgeHoldDuration)
			button.complete(button.started)
			button.MouseUp(primary)
			if window.Canvas().Overlays().Top() != nil {
				t.Fatal("hold must start removal without another confirmation")
			}
			waitForPage(t, window, "4. Done")
			expected := 1
			if scenario.purge {
				expected = 2
			}
			if len(backend.actions) != expected {
				t.Fatalf("executed %d actions, want %d", len(backend.actions), expected)
			}
			if scenario.err != nil {
				waitForPage(t, window, scenario.err.Error())
			} else {
				waitForPage(t, window, "All cleaned up")
			}
			assertCompletionCheck(t, window, scenario.err == nil)
			links := make(map[string]string)
			for _, object := range renderedObjects(window.Content()) {
				if link, ok := object.(*widget.Hyperlink); ok {
					links[link.Text] = link.URL.String()
				}
			}
			for label, target := range map[string]string{
				"PrintMaster website": "https://printmaster.work",
				"Documentation":       "https://docs.printmaster.work",
				"Support / GitHub":    "https://github.com/Printmaster-Org/printmaster",
			} {
				if links[label] != target {
					t.Fatalf("Done page link %q = %q, want %q", label, links[label], target)
				}
			}
			tapWizardButton(t, window.Content(), "Copy result")
			if application.Clipboard().Content() == "" {
				t.Fatal("copy result produced no report")
			}
			tapWizardButton(t, window.Content(), "Return to start")
			waitForPage(t, window, "1. Start")
			if backend.scanCalls != 1 {
				t.Fatal("returning to start must not automatically rescan")
			}
			tapWizardButton(t, window.Content(), "Scan this device")
			waitForPage(t, window, "2. Select")
		})
	}
}

func TestWizardScanErrorAndReadOnly(t *testing.T) {
	for _, scanError := range []bool{false, true} {
		application := test.NewTempApp(t)
		backend := wizardBackend()
		backend.readOnly = true
		if scanError {
			backend.scanError = errors.New("fixture scan failure")
		}
		window := BuildWindow(application, backend)
		t.Cleanup(window.Close)
		window.Show()
		tapWizardButton(t, window.Content(), "Scan this device")
		if scanError {
			waitForPage(t, window, "fixture scan failure")
			assertCompletionCheck(t, window, false)
		} else {
			waitForPage(t, window, "Read-only scan")
			for _, object := range renderedObjects(window.Content()) {
				if button, ok := object.(*widget.Button); ok && button.Text == "Review removal plan" && !button.Disabled() {
					t.Fatal("read-only scan allowed removal review")
				}
			}
		}
		if len(backend.actions) != 0 {
			t.Fatal("failed/read-only scan mutated the device")
		}
	}
}

func TestWizardSelectionAndFixedNavigation(t *testing.T) {
	application := test.NewTempApp(t)
	window := BuildWindow(application, wizardBackend())
	t.Cleanup(window.Close)
	window.Show()
	tapWizardButton(t, window.Content(), "Scan this device")
	waitForPage(t, window, "2. Select")
	var selected *widget.Check
	var review *widget.Button
	for _, object := range renderedObjects(window.Content()) {
		switch object := object.(type) {
		case *widget.Check:
			if object.Text == "Remove this installation" {
				selected = object
			}
		case *widget.Button:
			if object.Text == "Review removal plan" {
				review = object
			}
		}
	}
	if selected == nil || review == nil {
		t.Fatal("selection controls missing")
	}
	selected.SetChecked(false)
	if !review.Disabled() {
		t.Fatal("review must be disabled when nothing is selected")
	}
	selected.SetChecked(true)
	if review.Disabled() {
		t.Fatal("review did not enable after selecting an installation")
	}
	for _, width := range []float32{640, 900} {
		window.Resize(fyne.NewSize(width, 600))
		test.RenderToMarkup(window.Canvas())
		position := application.Driver().AbsolutePositionForObject(review)
		size := review.Size()
		canvas := window.Canvas().Size()
		if position.X < 0 || position.Y < 0 ||
			position.X+size.Width > canvas.Width || position.Y+size.Height > canvas.Height {
			t.Fatalf("navigation is clipped at width %g: position=%v size=%v canvas=%v", width, position, size, canvas)
		}
	}
}

func assertPageFits(t *testing.T, window fyne.Window) {
	t.Helper()
	test.RenderToMarkup(window.Canvas())
	for _, object := range renderedObjects(window.Content()) {
		if button, ok := object.(*widget.Button); ok && button.Visible() {
			position := fyne.CurrentApp().Driver().AbsolutePositionForObject(button)
			if position.Y+button.Size().Height > window.Canvas().Size().Height+1 {
				t.Fatalf("button %q is below the window: position=%v size=%v", button.Text, position, button.Size())
			}
		}
		if scroll, ok := object.(*container.Scroll); ok && scroll.Visible() {
			content, viewport := scroll.Content.Size(), scroll.Size()
			if content.Height > viewport.Height+1 || content.Width > viewport.Width+1 {
				t.Fatalf("default page needs scrolling: content=%v viewport=%v", content, viewport)
			}
		}
	}
}

func TestSingleInstallationPagesFitWithoutScrolling(t *testing.T) {
	for _, size := range []fyne.Size{fyne.NewSize(900, 760), fyne.NewSize(960, 820)} {
		for _, purge := range []bool{false, true} {
			t.Run(fmt.Sprintf("%gx%g_purge_%t", size.Width, size.Height, purge), func(t *testing.T) {
				application := test.NewTempApp(t)
				backend := wizardBackend()
				backend.inventory.Installations = []cleanup.Installation{{
					LayoutID: "windows-bootstrap-bin", Method: cleanup.MethodService,
					Version: "1.2.3", ServiceName: "PrintMasterAgent",
					Executables: []string{`C:\ProgramData\PrintMaster\bin\printmaster-agent.exe`},
					ConfigPaths: []string{`C:\ProgramData\PrintMaster\agent\config.toml`},
					DataPaths:   []string{`C:\ProgramData\PrintMaster\agent`},
					LogPaths:    []string{`C:\ProgramData\PrintMaster\agent\logs`},
					Processes:   []cleanup.Process{{PID: 1234, Path: `C:\ProgramData\PrintMaster\bin\printmaster-agent.exe`}},
					Evidence:    []string{"Known Agent service and executable"},
				}}
				window := BuildWindow(application, backend)
				t.Cleanup(window.Close)
				window.Resize(size)
				window.Show()
				assertPageFits(t, window)
				tapWizardButton(t, window.Content(), "Scan this device")
				waitForPage(t, window, "2. Select")
				assertPageFits(t, window)
				if purge {
					for _, object := range renderedObjects(window.Content()) {
						if check, ok := object.(*widget.Check); ok && check.Text == "Permanently purge Agent data" {
							check.SetChecked(true)
						}
					}
				}
				tapWizardButton(t, window.Content(), "Review removal plan")
				waitForPage(t, window, "3. Review")
				assertPageFits(t, window)
				var button *holdButton
				var details *widget.Accordion
				for _, object := range renderedObjects(window.Content()) {
					switch object := object.(type) {
					case *holdButton:
						button = object
					case *widget.ProgressBar:
						t.Fatal("review must not have a separate hold progress bar")
					case *widget.Accordion:
						details = object
					}
				}
				if button == nil || details == nil || details.Items[0].Open {
					t.Fatal("review must have a hold button and collapsed removal details")
				}
				details.Open(0)
				waitForPage(t, window, backend.inventory.Installations[0].Executables[0])
				details.Close(0)
				assertPageFits(t, window)
				button.started = time.Now().Add(-purgeHoldDuration)
				button.complete(button.started)
				waitForPage(t, window, "4. Done")
				assertPageFits(t, window)
			})
		}
	}
}
