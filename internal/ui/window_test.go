package ui

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/Printmaster-Org/printmaster-agent-cleanup/internal/cleanup"
)

type uiTestBackend struct {
	inventory cleanup.Inventory
	scanned   chan struct{}
	actions   []cleanup.Action
}

func (b *uiTestBackend) Scan(context.Context) (cleanup.Inventory, error) {
	close(b.scanned)
	return b.inventory, nil
}
func (b *uiTestBackend) IsElevated() bool { return true }
func (b *uiTestBackend) Execute(_ context.Context, action cleanup.Action) error {
	b.actions = append(b.actions, action)
	return nil
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
	if !strings.Contains(markup, "windows-legacy") || !strings.Contains(markup, "unknown service path") {
		t.Fatalf("wizard did not present inventory/unknown warning: %s", markup)
	}
	if len(backend.actions) != 0 {
		t.Fatalf("opening the wizard executed cleanup actions: %+v", backend.actions)
	}
	for _, object := range test.LaidOutObjects(window.Canvas().Content()) {
		if button, ok := object.(*widget.Button); ok && button.Text == "Remove selected Agent installs" && !button.Disabled() {
			t.Fatal("removal enabled with an unrecognized service entry")
		}
	}
}
