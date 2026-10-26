package cleanup

import (
	"context"
	"errors"
)

var ErrUnknownEntries = errors.New("unrecognized Agent entries require owner review")

// Platform identifies the host whose PrintMaster Agent installations are scanned.
type Platform string

const (
	PlatformWindows Platform = "windows"
	PlatformLinux   Platform = "linux"
	PlatformDarwin  Platform = "darwin"
)

// Method describes how an installation is owned by the operating system.
type Method string

const (
	MethodMSI         Method = "msi"
	MethodDeb         Method = "deb"
	MethodRPM         Method = "rpm"
	MethodService     Method = "service"
	MethodRawBinary   Method = "raw-binary"
	MethodInteractive Method = "interactive"
)

// Layout is a known historical or current installation shape. Paths are
// platform-native templates expanded by the platform inventory backend.
type Layout struct {
	ID              string
	Platform        Platform
	Era             string
	Method          Method
	PackageName     string
	MSIUpgradeCode  string
	ServiceNames    []string
	ExecutablePaths []string
	ConfigPaths     []string
	DataPaths       []string
	LogPaths        []string
}

// Installation is evidence that a known Agent layout exists on the host.
type Installation struct {
	LayoutID       string    `json:"layout_id"`
	Version        string    `json:"version,omitempty"`
	Method         Method    `json:"method"`
	PackageName    string    `json:"package_name,omitempty"`
	MSIProductCode string    `json:"msi_product_code,omitempty"`
	ServiceName    string    `json:"service_name,omitempty"`
	ServiceScope   string    `json:"service_scope,omitempty"`
	ServicePlist   string    `json:"service_plist,omitempty"`
	Executables    []string  `json:"executables,omitempty"`
	Processes      []Process `json:"processes,omitempty"`
	ConfigPaths    []string  `json:"config_paths,omitempty"`
	DataPaths      []string  `json:"data_paths,omitempty"`
	LogPaths       []string  `json:"log_paths,omitempty"`
	Evidence       []string  `json:"evidence,omitempty"`
}

type Process struct {
	PID  uint32 `json:"pid"`
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
}

// UnrecognizedEntry is report-only evidence that needs owner review. It is
// deliberately excluded from generated cleanup actions.
type UnrecognizedEntry struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Path   string `json:"path,omitempty"`
	Reason string `json:"reason"`
	PID    uint32 `json:"pid,omitempty"`
}

// Inventory contains recognized installations and report-only unknowns.
type Inventory struct {
	Platform      Platform            `json:"platform"`
	Installations []Installation      `json:"installations"`
	Unrecognized  []UnrecognizedEntry `json:"unrecognized,omitempty"`
}

type ActionKind string

const (
	ActionStopService      ActionKind = "stop-service"
	ActionStopProcess      ActionKind = "stop-process"
	ActionUninstallMSI     ActionKind = "uninstall-msi"
	ActionRemovePackage    ActionKind = "remove-package"
	ActionRemoveService    ActionKind = "remove-service"
	ActionRemoveExecutable ActionKind = "remove-executable"
	ActionPurgeConfig      ActionKind = "purge-config"
	ActionPurgeData        ActionKind = "purge-data"
	ActionPurgeLogs        ActionKind = "purge-logs"
)

// Action is an explicit, reviewable operation. Destructive data actions are
// absent from plans unless the caller explicitly requests purge.
type Action struct {
	Kind         ActionKind `json:"kind"`
	Target       string     `json:"target"`
	LayoutID     string     `json:"layout_id"`
	ProcessPath  string     `json:"process_path,omitempty"`
	Method       Method     `json:"method,omitempty"`
	ServiceScope string     `json:"service_scope,omitempty"`
	ServicePlist string     `json:"service_plist,omitempty"`
	Destructive  bool       `json:"destructive"`
	Reason       string     `json:"reason"`
}

// Plan separates application removal from optional user-data purge.
type Plan struct {
	Platform  Platform `json:"platform"`
	Actions   []Action `json:"actions"`
	Warnings  []string `json:"warnings,omitempty"`
	PurgeData bool     `json:"purge_data"`
}

// Backend inventories and executes actions using the platform's native
// package/service APIs. Inventory must never mutate the host.
type Backend interface {
	Scan(context.Context) (Inventory, error)
	Execute(context.Context, Action) error
	IsElevated() bool
}
