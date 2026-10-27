package ui

import (
	"fmt"
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func TestStartSplashIsVerticallyCentered(t *testing.T) {
	for _, size := range []fyne.Size{fyne.NewSize(900, 760), fyne.NewSize(960, 820), fyne.NewSize(900, 1000)} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			application := test.NewTempApp(t)
			window := BuildWindow(application, wizardBackend())
			t.Cleanup(window.Close)
			window.Resize(size)
			window.Show()
			test.RenderToMarkup(window.Canvas())
			assertPageFits(t, window)
			found := false
			hasTitle := false
			for _, object := range renderedObjects(window.Content()) {
				if title, ok := object.(*widget.RichText); ok && title.String() == "PrintMaster Agent Cleanup Tool" {
					hasTitle = true
				}
				if scroll, ok := object.(*container.Scroll); ok {
					centered, ok := scroll.Content.(*fyne.Container)
					if !ok {
						continue
					}
					if _, ok := centered.Layout.(verticalCenterLayout); !ok {
						continue
					}
					found = true
					splash := centered.Objects[0]
					top := splash.Position().Y
					bottom := scroll.Size().Height - top - splash.Size().Height
					if difference := top - bottom; difference < -1 || difference > 1 {
						t.Fatalf("splash not centered: top=%g bottom=%g viewport=%v", top, bottom, scroll.Size())
					}
				}
			}
			if !found || !hasTitle {
				t.Fatal("centered splash or capitalized product heading missing")
			}
		})
	}
}

func TestStepIndicatorAccentFollowsCurrentPageAndTheme(t *testing.T) {
	application := test.NewTempApp(t)
	window := BuildWindow(application, wizardBackend())
	t.Cleanup(window.Close)
	window.Show()
	assertAccent := func(activeText string) {
		t.Helper()
		count := 0
		for _, object := range renderedObjects(window.Content()) {
			step, ok := object.(*stepIndicator)
			if !ok {
				continue
			}
			count++
			renderer := test.WidgetRenderer(step).(*stepIndicatorRenderer)
			active := step.label.Text == activeText
			if step.active != active || step.label.TextStyle.Bold != active {
				t.Fatalf("wrong active step: %s", step.label.Text)
			}
			want := theme.Color(theme.ColorNameSeparator)
			if active {
				want = theme.Color(theme.ColorNamePrimary)
			}
			if color.NRGBAModel.Convert(renderer.line.FillColor) != color.NRGBAModel.Convert(want) {
				t.Fatalf("step %q underline has incorrect color", step.label.Text)
			}
			_, _, _, alpha := renderer.background.FillColor.RGBA()
			if (alpha > 0) != active {
				t.Fatalf("step %q highlight state incorrect", step.label.Text)
			}
			if renderer.line.Size().Width != step.Size().Width || renderer.line.Size().Height != 3 {
				t.Fatal("step underline must span the section width")
			}
		}
		if count != 4 {
			t.Fatalf("expected 4 step indicators, got %d", count)
		}
	}
	test.RenderToMarkup(window.Canvas())
	assertAccent("01  Start")
	tapWizardButton(t, window.Content(), "Scan this device")
	waitForPage(t, window, "2. Select")
	assertAccent("02  Select")
	tapWizardButton(t, window.Content(), "Review removal plan")
	waitForPage(t, window, "3. Review")
	assertAccent("03  Review")
	application.Settings().SetTheme(theme.LightTheme())
	test.RenderToMarkup(window.Canvas())
	assertAccent("03  Review")
}
