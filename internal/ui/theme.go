package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type cleanupTheme struct {
	fyne.Theme
}

func (t cleanupTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if variant == theme.VariantDark {
		switch name {
		case theme.ColorNameBackground:
			return color.NRGBA{R: 17, G: 23, B: 35, A: 255}
		case theme.ColorNameInputBackground:
			return color.NRGBA{R: 23, G: 31, B: 46, A: 255}
		case theme.ColorNameForeground:
			return color.NRGBA{R: 231, G: 237, B: 247, A: 255}
		case theme.ColorNameButton:
			return color.NRGBA{R: 38, G: 49, B: 68, A: 255}
		case theme.ColorNameDisabled, theme.ColorNamePlaceHolder:
			return color.NRGBA{R: 151, G: 166, B: 189, A: 255}
		case theme.ColorNameInputBorder, theme.ColorNameSeparator:
			return color.NRGBA{R: 49, G: 63, B: 84, A: 255}
		case theme.ColorNameForegroundOnPrimary:
			return color.NRGBA{R: 12, G: 23, B: 40, A: 255}
		}
	}
	if name == theme.ColorNamePrimary {
		if variant == theme.VariantDark {
			return color.NRGBA{R: 100, G: 163, B: 255, A: 255}
		}
		return color.NRGBA{R: 36, G: 94, B: 206, A: 255}
	}
	return t.Theme.Color(name, variant)
}

func (t cleanupTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 8
	case theme.SizeNameInnerPadding:
		return 8
	case theme.SizeNameText:
		return 14
	case theme.SizeNameHeadingText:
		return 26
	case theme.SizeNameSubHeadingText:
		return 18
	case theme.SizeNameButtonRadius, theme.SizeNameInputRadius:
		return 8
	case theme.SizeNameCardRadius:
		return 12
	}
	return t.Theme.Size(name)
}

func inset(content fyne.CanvasObject, padding float32) *fyne.Container {
	return container.New(layout.NewCustomPaddedLayout(padding, padding, padding, padding), content)
}

type verticalCenterLayout struct{}

func (verticalCenterLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return objects[0].MinSize()
}

func (verticalCenterLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	content := objects[0]
	height := content.MinSize().Height
	content.Resize(fyne.NewSize(size.Width, height))
	content.Move(fyne.NewPos(0, max(0, (size.Height-height)/2)))
}

func section(title, description string, objects ...fyne.CanvasObject) fyne.CanvasObject {
	titleText := widget.NewRichText(&widget.TextSegment{
		Text: title, Style: widget.RichTextStyle{Alignment: fyne.TextAlignCenter, TextStyle: fyne.TextStyle{Bold: true}, SizeName: theme.SizeNameSubHeadingText},
	})
	content := container.NewVBox(titleText)
	if description != "" {
		content.Add(wrappedLabel(description))
	}
	for _, object := range objects {
		content.Add(object)
	}
	return widget.NewCard("", "", inset(content, 6))
}
