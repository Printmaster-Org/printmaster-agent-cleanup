//go:build !windows

package main

import "fmt"

func ensureElevatedForGUI() (bool, error) { return false, nil }

func showStartupError(err error) { fmt.Println(err) }
