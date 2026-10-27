package ui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestHoldProgressFillsButtonBehindLabel(t *testing.T) {
	test.NewTempApp(t)
	button := newHoldButton(func() { t.Fatal("drawing progress must not confirm") })
	button.Resize(fyne.NewSize(220, 48))
	renderer := test.WidgetRenderer(button).(*holdButtonRenderer)
	for _, progress := range []float32{0, 0.25, 0.5, 1} {
		button.setProgress(progress)
		if got, want := renderer.fill.Size().Width, button.Size().Width*progress; got != want {
			t.Fatalf("progress %g fills %g pixels, want %g", progress, got, want)
		}
		if renderer.fill.Size().Height != button.Size().Height {
			t.Fatal("progress must fill the button height")
		}
		if renderer.label.Text != "Hold to Confirm" || renderer.label.Position().Y < 0 {
			t.Fatal("hold label must remain visible and unchanged")
		}
		objects := renderer.Objects()
		if len(objects) != 3 || objects[1] != renderer.fill || objects[2] != renderer.label {
			t.Fatal("progress must be inside the button, behind its label")
		}
	}
	button.cancel()
	if renderer.fill.Size().Width != 0 {
		t.Fatal("cancellation must clear the in-button fill")
	}
}

func TestCancelledHoldCannotCompleteLater(t *testing.T) {
	test.NewTempApp(t)
	calls := 0
	button := newHoldButton(func() { calls++ })
	stale := time.Now().Add(-purgeHoldDuration)
	button.started = stale
	button.cancel()
	button.complete(stale)
	button.started = time.Now()
	button.complete(stale)
	if calls != 0 {
		t.Fatal("a cancelled hold must not confirm or complete a newer hold")
	}
	button.started = stale
	button.complete(stale)
	button.complete(stale)
	if calls != 1 {
		t.Fatalf("completed hold confirmed %d times, want exactly once", calls)
	}
}
