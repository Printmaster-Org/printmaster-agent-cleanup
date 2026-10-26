package main

import (
	"fmt"
	"os"

	"fyne.io/fyne/v2/app"
	"github.com/Printmaster-Org/printmaster-agent-cleanup/internal/cleanup"
	"github.com/Printmaster-Org/printmaster-agent-cleanup/internal/ui"
)

func main() {
	backend := cleanup.NewBackend()
	if len(os.Args) == 1 || os.Args[1] == "gui" || os.Args[1] == "--gui" {
		application := app.NewWithID("work.printmaster.agent-cleanup")
		window := ui.BuildWindow(application, backend)
		window.ShowAndRun()
		return
	}
	if err := runCLI(os.Args[1:], backend, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "agent-cleanup:", err)
		os.Exit(2)
	}
}
