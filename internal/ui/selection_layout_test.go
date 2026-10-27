package ui

import (
	"fmt"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/Printmaster-Org/printmaster-agent-cleanup/internal/cleanup"
)

func TestTwoInstallationSelectionKeepsDataPolicyVisible(t *testing.T) {
	for _, size := range []fyne.Size{fyne.NewSize(900, 760), fyne.NewSize(960, 820)} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			application := test.NewTempApp(t)
			backend := wizardBackend()
			backend.inventory.Installations = []cleanup.Installation{
				{LayoutID: "windows-bootstrap-bin", Method: cleanup.MethodService, ServiceName: "PrintMasterAgent",
					Executables: []string{`C:\ProgramData\PrintMaster\bin\printmaster-agent.exe`}},
				{LayoutID: "windows-msi-protected", Method: cleanup.MethodRawBinary,
					Executables: []string{`C:\Program Files\PrintMaster\Agent\printmaster-agent.exe`}},
			}
			window := BuildWindow(application, backend)
			t.Cleanup(window.Close)
			window.Resize(size)
			window.Show()
			tapWizardButton(t, window.Content(), "Scan this device")
			waitForPage(t, window, "2. Select")
			assertPageFits(t, window)
			var choices []*widget.Check
			var purge *widget.Check
			for _, object := range renderedObjects(window.Content()) {
				if check, ok := object.(*widget.Check); ok {
					if check.Text == "Permanently purge Agent data" {
						purge = check
					} else {
						choices = append(choices, check)
					}
				}
			}
			if len(choices) != 2 || purge == nil || purge.Disabled() {
				t.Fatal("installation and data choices must be available")
			}
			first := application.Driver().AbsolutePositionForObject(choices[0])
			second := application.Driver().AbsolutePositionForObject(choices[1])
			if first.Y != second.Y || first.X >= second.X {
				t.Fatalf("installation cards must be side-by-side: %v %v", first, second)
			}
			position := application.Driver().AbsolutePositionForObject(purge)
			if position.Y+purge.Size().Height > window.Canvas().Size().Height {
				t.Fatal("purge checkbox is offscreen")
			}
			backend.inventory.Installations = append(backend.inventory.Installations,
				backend.inventory.Installations[0], backend.inventory.Installations[1],
				backend.inventory.Installations[0], backend.inventory.Installations[1])
			tapWizardButton(t, window.Content(), "Scan again")
			waitForPage(t, window, "Scan complete · 6 recognized")
			waitForPage(t, window, "2. Select")
			for _, object := range renderedObjects(window.Content()) {
				if check, ok := object.(*widget.Check); ok && check.Text == "Permanently purge Agent data" {
					purge = check
				}
			}
			position = application.Driver().AbsolutePositionForObject(purge)
			if position.Y+purge.Size().Height > window.Canvas().Size().Height {
				t.Fatal("purge option must remain visible when installations overflow")
			}
			purge.SetChecked(true)
			tapWizardButton(t, window.Content(), "Review removal plan")
			waitForPage(t, window, "PERMANENT DATA PURGE")
		})
	}
}

func TestStartHasCenteredScanAndStepHeadings(t *testing.T) {
	application := test.NewTempApp(t)
	window := BuildWindow(application, wizardBackend())
	t.Cleanup(window.Close)
	window.Show()
	waitForPage(t, window, "1. Start")
	test.RenderToMarkup(window.Canvas())
	found := false
	for _, object := range renderedObjects(window.Content()) {
		if button, ok := object.(*widget.Button); ok && button.Text == "Scan this device" {
			found = true
			center := application.Driver().AbsolutePositionForObject(button).X + button.Size().Width/2
			if offset := center - window.Canvas().Size().Width/2; offset < -1 || offset > 1 {
				t.Fatal("start scan button is not centered")
			}
		}
		if label, ok := object.(*widget.Label); ok {
			for _, step := range []string{"01  Start", "02  Select", "03  Review", "04  Done", "1. Start"} {
				if label.Text == step && label.Alignment != fyne.TextAlignCenter {
					t.Fatalf("heading %q is not centered", step)
				}
			}
		}
		if title, ok := object.(*widget.RichText); ok && title.String() == "A clean start for your Agent" {
			if title.Segments[0].(*widget.TextSegment).Style.Alignment != fyne.TextAlignCenter {
				t.Fatal("section heading is not centered")
			}
		}
	}
	if !found {
		t.Fatal("start scan button missing")
	}
}
