package main

import (
	"errors"
	"fmt"
	"os"

	"fyne.io/fyne/v2/app"
	"github.com/Printmaster-Org/printmaster-agent-cleanup/internal/cleanup"
	"github.com/Printmaster-Org/printmaster-agent-cleanup/internal/ui"
)

var errElevationDeclined = errors.New("administrator elevation was declined")

func main() {
	backend := cleanup.NewBackend()
	args := os.Args[1:]
	if shouldOpenGUI(args) {
		relaunched, err := ensureElevatedForGUI()
		if err != nil {
			if !errors.Is(err, errElevationDeclined) {
				showStartupError(err)
			}
			return
		}
		if relaunched {
			return
		}
		application := app.NewWithID("work.printmaster.agent-cleanup")
		window := ui.BuildWindow(application, backend)
		window.ShowAndRun()
		return
	}
	if err := attachConsoleForCLI(); err != nil {
		showStartupError(err)
		return
	}
	if len(args) == 0 {
		args = []string{"help"}
	}
	if err := runCLI(args, backend, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "agent-cleanup:", err)
		os.Exit(2)
	}
}

func shouldOpenGUI(args []string) bool {
	return len(args) == 0 || args[0] == "gui" || args[0] == "--gui"
}
