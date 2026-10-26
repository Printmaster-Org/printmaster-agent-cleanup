package cleanup

// KnownLayouts documents installation locations seen across PrintMaster
// generations. Platform backends expand environment/profile placeholders and
// report only paths that actually exist.
func KnownLayouts(platform Platform) []Layout {
	switch platform {
	case PlatformWindows:
		return []Layout{
			{
				ID: "windows-msi-protected", Platform: PlatformWindows, Era: "current MSI", Method: MethodMSI,
				MSIUpgradeCode:  "{E1F8F20A-5D36-4DF8-879B-A868A69243AC}",
				ServiceNames:    []string{"PrintMasterAgent"},
				ExecutablePaths: []string{`%ProgramFiles%\PrintMaster\Agent\printmaster-agent.exe`},
				ConfigPaths:     []string{`%ProgramFiles%\PrintMaster\Agent\config`},
				DataPaths:       []string{`%ProgramData%\PrintMaster\agent`},
			},
			{
				ID: "windows-legacy-msi-versioned", Platform: PlatformWindows, Era: "legacy MSI", Method: MethodMSI,
				MSIUpgradeCode:  "{E1F8F20A-5D36-4DF8-879B-A868A69243AC}",
				ServiceNames:    []string{"PrintMasterAgent"},
				ExecutablePaths: []string{`%ProgramFiles%\PrintMaster\printmaster-agent.exe`, `%ProgramFiles%\PrintMaster\printmaster-agent-v*.exe`},
				ConfigPaths:     []string{`%ProgramData%\PrintMaster\agent\config.toml`, `%ProgramData%\PrintMaster\config.toml`},
				DataPaths:       []string{`%ProgramData%\PrintMaster\agent`},
			},
			{
				ID: "windows-bootstrap-bin", Platform: PlatformWindows, Era: "Server bootstrap", Method: MethodRawBinary,
				ServiceNames:    []string{"PrintMasterAgent"},
				ExecutablePaths: []string{`%ProgramData%\PrintMaster\bin\printmaster-agent.exe`},
				ConfigPaths:     []string{`%ProgramData%\PrintMaster\agent\config.toml`},
				DataPaths:       []string{`%ProgramData%\PrintMaster\agent`},
			},
			{
				ID: "windows-user-data", Platform: PlatformWindows, Era: "interactive/user", Method: MethodInteractive,
				DataPaths: []string{`%LOCALAPPDATA%\PrintMaster\agent`},
			},
		}
	case PlatformLinux:
		return []Layout{
			{
				ID: "linux-deb", Platform: PlatformLinux, Era: "current Debian package", Method: MethodDeb,
				PackageName: "printmaster-agent", ServiceNames: []string{"printmaster-agent.service"},
				ExecutablePaths: []string{"/usr/bin/printmaster-agent"},
				ConfigPaths:     []string{"/etc/printmaster/agent.toml"},
				DataPaths:       []string{"/var/lib/printmaster/agent"},
				LogPaths:        []string{"/var/log/printmaster"},
			},
			{
				ID: "linux-rpm", Platform: PlatformLinux, Era: "current RPM package", Method: MethodRPM,
				PackageName: "printmaster-agent", ServiceNames: []string{"printmaster-agent.service"},
				ExecutablePaths: []string{"/usr/bin/printmaster-agent"},
				ConfigPaths:     []string{"/etc/printmaster/agent.toml"},
				DataPaths:       []string{"/var/lib/printmaster/agent"},
				LogPaths:        []string{"/var/log/printmaster"},
			},
			{
				ID: "linux-direct-pm-agent", Platform: PlatformLinux, Era: "legacy direct bootstrap", Method: MethodService,
				ServiceNames:    []string{"printmaster-agent.service", "PrintMasterAgent.service"},
				ExecutablePaths: []string{"/usr/local/bin/pm-agent"},
				ConfigPaths:     []string{"/etc/printmaster/config.toml"},
				DataPaths:       []string{"/var/lib/printmaster/agent"},
			},
			{
				ID: "linux-direct-printmaster-agent", Platform: PlatformLinux, Era: "legacy direct binary", Method: MethodRawBinary,
				ServiceNames:    []string{"PrintMasterAgent.service"},
				ExecutablePaths: []string{"/usr/local/bin/printmaster-agent"},
				ConfigPaths:     []string{"/etc/printmaster/config.toml"},
				DataPaths:       []string{"/var/lib/printmaster/agent"},
			},
			{
				ID: "linux-user-data", Platform: PlatformLinux, Era: "interactive/user", Method: MethodInteractive,
				DataPaths: []string{"~/.local/share/printmaster/agent"},
			},
		}
	case PlatformDarwin:
		return []Layout{
			{
				ID: "darwin-service", Platform: PlatformDarwin, Era: "service", Method: MethodService,
				ServiceNames: []string{"PrintMasterAgent"},
				DataPaths:    []string{"/var/lib/printmaster/agent", "/Library/Application Support/PrintMaster/agent"},
				LogPaths:     []string{"/var/log/printmaster"},
			},
			{
				ID: "darwin-user-data", Platform: PlatformDarwin, Era: "interactive/user", Method: MethodInteractive,
				DataPaths: []string{"~/Library/Application Support/PrintMaster/agent"},
			},
		}
	default:
		return nil
	}
}
