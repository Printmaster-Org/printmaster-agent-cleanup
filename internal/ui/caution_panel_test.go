package ui

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func TestDataCautionPanelHighlightsIrreversibleOptIn(t *testing.T) {
	application := test.NewTempApp(t)
	window := BuildWindow(application, wizardBackend())
	t.Cleanup(window.Close)
	window.Show()
	tapWizardButton(t, window.Content(), "Scan this device")
	waitForPage(t, window, "2. Select")
	for _, currentTheme := range []fyne.Theme{theme.LightTheme(), theme.DarkTheme()} {
		application.Settings().SetTheme(currentTheme)
		test.RenderToMarkup(window.Canvas())
		found := false
		for _, object := range renderedObjects(window.Content()) {
			if panel, ok := object.(*cautionPanel); ok {
				found = true
				renderer := test.WidgetRenderer(panel).(*cautionPanelRenderer)
				if color.NRGBAModel.Convert(renderer.background.StrokeColor) != color.NRGBAModel.Convert(theme.Color(theme.ColorNameWarning)) {
					t.Fatal("caution border must follow theme warning color")
				}
				_, _, _, alpha := renderer.background.FillColor.RGBA()
				if alpha == 0 || renderer.background.StrokeWidth == 0 {
					t.Fatal("caution panel needs a visible tint and border")
				}
			}
			if check, ok := object.(*widget.Check); ok && check.Text == "Permanently purge Agent data" && check.Checked {
				t.Fatal("highlighting purge must not opt the user into deletion")
			}
		}
		if !found {
			t.Fatal("data caution panel missing")
		}
	}
}
