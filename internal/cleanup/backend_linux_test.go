//go:build linux

package cleanup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxUserInstallTargetsAreAllowed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	executable := filepath.Join(home, ".local", "bin", "printmaster-agent")
	data := filepath.Join(home, ".local", "share", "printmaster", "agent")
	if !isKnownLinuxExecutable(executable, knownLinuxExecutablePaths()) {
		t.Fatalf("known per-user executable was rejected: %s", executable)
	}
	if !isKnownLinuxDataPath(data) {
		t.Fatalf("known per-user Agent data path was rejected: %s", data)
	}
	if isKnownLinuxDataPath(filepath.Join(home, ".local", "share", "printmaster")) {
		t.Fatal("shared PrintMaster user data root must not be purgeable")
	}
}

func TestLinuxRegularFileRemovalRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "printmaster-agent")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := removeRegularFile(link); err == nil {
		t.Fatal("symlink executable was removed")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target was altered: %v", err)
	}
}

func TestLinuxAgentProcessDetectionHandlesTruncatedNamesAndLegacyBinary(t *testing.T) {
	cases := []struct {
		name, path string
		want       bool
	}{
		{"printmaster-agen", "/usr/bin/printmaster-agent", true},
		{"pm-agent", "/usr/local/bin/pm-agent", true},
		{"other", "/tmp/printmaster-agent", false},
		{"printmaster-agent", "", true},
	}
	for _, test := range cases {
		if got := isLinuxAgentProcess(test.name, test.path); got != test.want {
			t.Errorf("isLinuxAgentProcess(%q, %q) = %t, want %t", test.name, test.path, got, test.want)
		}
	}
}
