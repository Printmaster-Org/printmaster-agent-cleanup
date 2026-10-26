# PrintMaster Agent Cleanup

PrintMaster Agent Cleanup is a standalone recovery utility for removing known legacy and current PrintMaster Agent installations. It starts with a graphical scan-and-review wizard; explicit CLI commands are available for automation and remote support.

The utility removes only Agent-owned services, package registrations, installer registrations, and executable files it can positively identify. Agent configuration, identity, databases, credentials, logs, and update trust are preserved unless data purge is explicitly selected. Shared PrintMaster Server directories and accounts are outside its scope.

## Safety model

- Launch with no arguments to open the GUI. The first operation is a read-only scan.
- Review the recognized install paths and evidence before selecting removal.
- Unknown Agent-named services, processes, or Windows uninstall entries are report-only and block all removal until reviewed by the owner.
- Removal requires Administrator/root privileges and an explicit confirmation.
- Purging Agent data is a separate opt-in. The GUI requires typing `PURGE PRINTMASTER AGENT DATA`; the CLI requires `--purge-data` in addition to `--yes`.
- Each purge target is checked against a platform-specific allowlist. Symlinks and linked descendants are refused.
- Package managers and Windows Installer remain responsible for files they own. The tool does not reboot the device or remove shared Server state.

This is a recovery utility, not an unattended fleet-management or remote-execution tool. Test it against representative disposable installations before broad deployment. Keep a backup when Agent identity or queued state may be needed for recovery.

## Recognized installation families

The scanner checks known paths and native ownership metadata rather than searching or deleting broad PrintMaster directories.

| Platform | Recognized families |
| --- | --- |
| Windows | Current and versioned legacy MSI products matching the PrintMaster Agent UpgradeCode; the Server bootstrap binary under `%ProgramData%`; legacy service/raw binaries under PrintMaster Program Files locations; and the current user's LocalAppData Agent layout. |
| Linux | `printmaster-agent` DEB/RPM packages; legacy `pm-agent` and `printmaster-agent` direct binaries under `/usr/local/bin`; known systemd units executing those exact binaries; and the current user's `.local` binary/data layout. |
| macOS | Known `/usr/local/bin` and Homebrew binaries, the system LaunchDaemon, the current user's LaunchAgent, and the current user's supported interactive data layout. |

Data discovery is intentionally scoped to the installation that owns the executable or service. Orphaned known per-user data is shown separately. Paths outside these families are not removed. macOS coverage is best-effort until validated against representative historical installs.

## CLI

```text
agent-cleanup scan [--json]
agent-cleanup plan [--purge-data]
agent-cleanup remove --yes [--purge-data]
```

`scan` does not mutate the device. `plan` prints the operations that would run. Review the plan before `remove`; `remove` scans again immediately before execution. Unknown inventory blocks the entire removal plan. `--json` is supported by `scan` only.

## Build and test

Use Go 1.27.3 or newer. Fyne's native desktop driver requires a C compiler and platform graphics development libraries.

```sh
go test ./...
go vet ./...
go build -o agent-cleanup ./cmd/agent-cleanup
```

On headless Linux development environments without CGO or graphics libraries, Fyne's test driver can be selected with:

```sh
CGO_ENABLED=0 go test -tags ci ./...
```

Native release builds are made on Windows, Linux, and macOS runners. Release assets are named by platform and architecture, and `SHA256SUMS` is published alongside them.

## Recovery notes

- Removing the Agent does not remove its configuration or state by default. A fresh install may reuse the existing identity and update-trust state.
- Purging state is irreversible. Export any needed diagnostics and preserve required identity before selecting it.
- Unknown entries should be investigated with their owning installer or service configuration. Do not broaden the allowlists merely to make an unfamiliar installation removable.
- The tool does not delete the shared `/var/lib/printmaster` root, shared PrintMaster accounts, Server data, or arbitrary user directories.