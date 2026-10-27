package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type cautionPanel struct {
	widget.BaseWidget
	content fyne.CanvasObject
}

func newDataCautionPanel(choice *widget.Check) *cautionPanel {
	title := widget.NewRichText(&widget.TextSegment{
		Text: "Agent data - use caution",
		Style: widget.RichTextStyle{Alignment: fyne.TextAlignCenter, ColorName: theme.ColorNameWarning,
			TextStyle: fyne.TextStyle{Bold: true}, SizeName: theme.SizeNameSubHeadingText},
	})
	panel := &cautionPanel{content: inset(container.NewVBox(
		container.NewStack(title, container.NewBorder(nil, nil,
			widget.NewIcon(theme.NewWarningThemedResource(theme.WarningIcon())), nil, nil)),
		wrappedLabel("Data is kept by default. Purge permanently deletes Agent configuration, identity, databases, credentials, logs, and update trust. This cannot be undone."),
		choice,
	), 14)}
	panel.ExtendBaseWidget(panel)
	return panel
}

func (p *cautionPanel) CreateRenderer() fyne.WidgetRenderer {
	background := canvas.NewRectangle(color.Transparent)
	background.CornerRadius = 12
	background.StrokeWidth = 1
	content := container.NewStack(background, p.content)
	renderer := &cautionPanelRenderer{content: content, background: background}
	renderer.Refresh()
	return renderer
}

type cautionPanelRenderer struct {
	content    *fyne.Container
	background *canvas.Rectangle
}

func (r *cautionPanelRenderer) Layout(size fyne.Size) { r.content.Resize(size) }
func (r *cautionPanelRenderer) MinSize() fyne.Size    { return r.content.MinSize() }
func (r *cautionPanelRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.content}
}
func (r *cautionPanelRenderer) Destroy() {}
func (r *cautionPanelRenderer) Refresh() {
	warning := theme.Color(theme.ColorNameWarning)
	red, green, blue, _ := warning.RGBA()
	r.background.FillColor = color.NRGBA{R: uint8(red >> 8), G: uint8(green >> 8), B: uint8(blue >> 8), A: 22}
	r.background.StrokeColor = warning
	r.background.Refresh()
	r.content.Refresh()
}
