//go:build windows

package cleanup

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsProcessExitFixture(t *testing.T) {
	if os.Getenv("CLEANUP_PROCESS_FIXTURE") != "1" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

func TestWindowsAlreadyExitedProcessNeedsNoTerminationPermission(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestWindowsProcessExitFixture$")
	command.Env = append(os.Environ(), "CLEANUP_PROCESS_FIXTURE=1")
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		input.Close()
		if command.ProcessState == nil {
			_ = command.Wait()
		}
	})
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(command.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if exited, err := windowsProcessExited(handle); err != nil || exited {
		t.Fatalf("fixture should be running: exited=%t err=%v", exited, err)
	}
	if err := stopWindowsProcess(ctx, fmt.Sprint(command.Process.Pid), `C:\unknown\agent.exe`); err == nil {
		t.Fatal("running process with an unrecognized path must not be stopped")
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	if exited, err := windowsProcessExited(handle); err != nil || !exited {
		t.Fatalf("fixture should be exited: exited=%t err=%v", exited, err)
	}
	if err := stopWindowsProcess(ctx, fmt.Sprint(command.Process.Pid), executable); err != nil {
		t.Fatalf("already exited process should not require termination rights: %v", err)
	}
}
