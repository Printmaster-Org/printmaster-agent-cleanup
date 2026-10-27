package ui

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/Printmaster-Org/printmaster-agent-cleanup/internal/cleanup"
)

type uiTestBackend struct {
	inventory    cleanup.Inventory
	scanned      chan struct{}
	actions      []cleanup.Action
	scanOnce     sync.Once
	scanError    error
	executeError error
	readOnly     bool
}

func (b *uiTestBackend) Scan(context.Context) (cleanup.Inventory, error) {
	b.scanOnce.Do(func() { close(b.scanned) })
	return b.inventory, b.scanError
}
func (b *uiTestBackend) IsElevated() bool { return !b.readOnly }
func (b *uiTestBackend) Execute(_ context.Context, action cleanup.Action) error {
	b.actions = append(b.actions, action)
	return b.executeError
}

func TestWizardScansWithoutMutationAndBlocksUnknownEntries(t *testing.T) {
	application := test.NewTempApp(t)
	backend := &uiTestBackend{
		inventory: cleanup.Inventory{
			Platform:      cleanup.PlatformWindows,
			Installations: []cleanup.Installation{{LayoutID: "windows-legacy", Method: cleanup.MethodMSI, MSIProductCode: "{fixture}"}},
			Unrecognized:  []cleanup.UnrecognizedEntry{{Kind: "service", Name: "PrintMasterAgent", Path: `C:\custom\agent.exe`, Reason: "unknown service path"}},
		},
		scanned: make(chan struct{}),
	}

	window := BuildWindow(application, backend)
	t.Cleanup(window.Close)
	tapWizardButton(t, window.Content(), "Scan this device")
	select {
	case <-backend.scanned:
	case <-time.After(3 * time.Second):
		t.Fatal("wizard did not scan the host")
	}
	deadline := time.Now().Add(3 * time.Second)
	markup := ""
	for time.Now().Before(deadline) {
		fyne.DoAndWait(func() {})
		markup = test.RenderObjectToMarkup(window.Canvas().Content())
		if strings.Contains(markup, "unknown service path") {
			break
		}
		runtime.Gosched()
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(markup, "PrintMaster Agent") || !strings.Contains(markup, "unknown service path") {
		t.Fatalf("wizard did not present inventory/unknown warning: %s", markup)
	}
	if len(backend.actions) != 0 {
		t.Fatalf("opening the wizard executed cleanup actions: %+v", backend.actions)
	}
	for _, object := range test.LaidOutObjects(window.Canvas().Content()) {
		if button, ok := object.(*widget.Button); ok && button.Text == "Review removal plan" && !button.Disabled() {
			t.Fatal("removal enabled with an unrecognized service entry")
		}
	}
}

func showHoldTestControls(parent fyne.Window, onConfirmed func()) {
	button := newHoldButton(onConfirmed)
	parent.SetContent(container.NewVBox(button, widget.NewButton("Cancel", button.cancel)))
}

func TestRemovalHoldLayout(t *testing.T) {
	for _, width := range []float32{420, 900} {
		t.Run(fmt.Sprintf("width_%g", width), func(t *testing.T) {
			application := test.NewTempApp(t)
			window := application.NewWindow("Purge confirmation test")
			t.Cleanup(window.Close)
			window.Resize(fyne.NewSize(width, 600))
			window.Show()
			showHoldTestControls(window, func() {
				t.Fatal("showing the dialog must not confirm a purge")
			})

			popup := window.Content()
			var button *holdButton
			for _, object := range renderedObjects(popup) {
				switch object := object.(type) {
				case *holdButton:
					button = object
				case *widget.ProgressBar:
					t.Fatal("hold progress must be inside the button, not a separate bar")
				case *widget.Entry:
					t.Fatal("purge confirmation must not require typing")
				}
			}
			if button == nil || button.Text != "Hold to Confirm" {
				t.Fatal("confirmation must show a Hold to Confirm button")
			}
			for _, object := range []fyne.CanvasObject{button} {
				position := application.Driver().AbsolutePositionForObject(object)
				size := object.Size()
				canvasSize := window.Canvas().Size()
				if position.X < 0 || position.Y < 0 ||
					position.X+size.Width > canvasSize.Width || position.Y+size.Height > canvasSize.Height {
					t.Fatalf("%T is clipped: position %v, size %v, canvas %v", object, position, size, canvasSize)
				}
				if size.Width < object.MinSize().Width || size.Height < object.MinSize().Height {
					t.Fatalf("%T is smaller than its readable minimum: %v < %v", object, size, object.MinSize())
				}
			}
		})
	}
}

func TestRemovalCancelsHold(t *testing.T) {
	for _, action := range []string{"release early", "leave button", "lose focus", "cancel", "click", "space tap", "secondary click", "repeated key down"} {
		t.Run(action, func(t *testing.T) {
			application := test.NewTempApp(t)
			window := application.NewWindow("Purge confirmation test")
			t.Cleanup(window.Close)
			window.Resize(fyne.NewSize(900, 600))
			window.Show()
			calls := 0
			showHoldTestControls(window, func() { calls++ })
			popup := window.Content()
			var button *holdButton
			var cancel *widget.Button
			for _, object := range renderedObjects(popup) {
				switch object := object.(type) {
				case *holdButton:
					button = object
				case *widget.Button:
					if object.Text == "Cancel" {
						cancel = object
					}
				}
			}
			if button == nil || cancel == nil {
				t.Fatal("missing confirmation controls")
			}
			primary := &desktop.MouseEvent{Button: desktop.MouseButtonPrimary}
			space := &fyne.KeyEvent{Name: fyne.KeySpace}
			switch action {
			case "click":
				test.Tap(button)
			case "space tap":
				button.TypedKey(space)
			case "secondary click":
				secondary := &desktop.MouseEvent{Button: desktop.MouseButtonSecondary}
				button.MouseDown(secondary)
				button.MouseUp(secondary)
			case "repeated key down":
				button.KeyDown(space)
				started := button.started
				button.KeyDown(space)
				if button.started != started {
					t.Fatal("key repeat restarted the hold")
				}
				button.KeyUp(space)
			default:
				button.MouseDown(primary)
				if window.Canvas().Focused() != button {
					t.Fatal("mouse hold must take focus so losing window focus cancels it")
				}
				button.setProgress(0.5)
				switch action {
				case "release early":
					button.MouseUp(primary)
				case "leave button":
					button.MouseOut()
				case "lose focus":
					window.Canvas().Unfocus()
				case "cancel":
					test.Tap(cancel)
				}
				button.MouseUp(primary)
			}
			if calls != 0 || !button.started.IsZero() || button.progress != 0 {
				t.Fatalf("cancelled hold did not reset safely: calls=%d, started=%v, progress=%g", calls, button.started, button.progress)
			}
		})
	}
}

func TestRemovalRequiresThreeSecondHold(t *testing.T) {
	application := test.NewTempApp(t)
	window := application.NewWindow("Purge confirmation test")
	t.Cleanup(window.Close)
	window.Resize(fyne.NewSize(900, 600))
	window.Show()
	calls := 0
	showHoldTestControls(window, func() { calls++ })
	popup := window.Content()
	var button *holdButton
	for _, object := range renderedObjects(popup) {
		if hold, ok := object.(*holdButton); ok {
			button = hold
		}
	}
	if button == nil {
		t.Fatal("missing hold button")
	}
	primary := &desktop.MouseEvent{Button: desktop.MouseButtonPrimary}
	button.MouseDown(primary)
	time.Sleep(3100 * time.Millisecond)
	fyne.DoAndWait(func() {
		if calls != 1 {
			t.Fatalf("hold must confirm automatically without release, got %d calls", calls)
		}
		if button.progress != 1 {
			t.Fatalf("completed hold must show full progress, got %g", button.progress)
		}
		button.MouseUp(primary)
		button.MouseUp(primary)
	})
	if calls != 1 {
		t.Fatalf("completed hold called removal %d times, want 1", calls)
	}
}

func TestHoldButtonKeyboardThreshold(t *testing.T) {
	test.NewTempApp(t)
	for _, elapsed := range []time.Duration{purgeHoldDuration - 100*time.Millisecond, purgeHoldDuration} {
		t.Run(elapsed.String(), func(t *testing.T) {
			calls := 0
			button := newHoldButton(func() { calls++ })
			button.started = time.Now().Add(-elapsed)
			button.keyboard = true
			button.complete(button.started)
			button.KeyUp(&fyne.KeyEvent{Name: fyne.KeySpace})
			want := 0
			if elapsed == purgeHoldDuration {
				want = 1
			}
			if calls != want {
				t.Fatalf("hold of %v called review %d times, want %d", elapsed, calls, want)
			}
		})
	}
}

func renderedObjects(object fyne.CanvasObject) []fyne.CanvasObject {
	objects := []fyne.CanvasObject{object}
	var children []fyne.CanvasObject
	switch object := object.(type) {
	case *fyne.Container:
		children = object.Objects
	case fyne.Widget:
		children = test.WidgetRenderer(object).Objects()
	}
	for _, child := range children {
		objects = append(objects, renderedObjects(child)...)
	}
	return objects
}
