package ui

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed icon.svg
var iconSVG []byte

var appIcon = fyne.NewStaticResource("printmaster-icon.svg", iconSVG)
