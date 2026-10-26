//go:build !windows && !linux && !darwin

package cleanup

import (
	"context"
	"errors"
)

type unsupportedBackend struct{}

func NewBackend() Backend { return unsupportedBackend{} }

func (unsupportedBackend) IsElevated() bool { return false }

func (unsupportedBackend) Scan(context.Context) (Inventory, error) {
	return Inventory{}, errors.New("PrintMaster Agent cleanup is supported on Windows, Linux, and macOS")
}

func (unsupportedBackend) Execute(context.Context, Action) error {
	return errors.New("PrintMaster Agent cleanup is supported on Windows, Linux, and macOS")
}
