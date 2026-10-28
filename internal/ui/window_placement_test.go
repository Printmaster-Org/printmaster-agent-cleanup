package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

type placementApp struct {
	fyne.App
	window *placementWindow
}

func (a *placementApp) NewWindow(title string) fyne.Window {
	a.window = &placementWindow{Window: a.App.NewWindow(title)}
	return a.window
}

type placementWindow struct {
	fyne.Window
	centered bool
}

func (w *placementWindow) CenterOnScreen() {
	w.centered = w.Content() != nil && w.Canvas().Size().Width >= 960 && w.Canvas().Size().Height >= 820
	w.Window.CenterOnScreen()
}

func TestWindowCentersAfterInitialLayout(t *testing.T) {
	application := &placementApp{App: test.NewTempApp(t)}
	window := BuildWindow(application, wizardBackend())
	t.Cleanup(window.Close)
	if !application.window.centered {
		t.Fatal("window must request screen centering after sizing and initial layout")
	}
}
