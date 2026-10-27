package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type stepIndicator struct {
	widget.BaseWidget
	label  *widget.Label
	active bool
}

func newStepIndicator(text string) *stepIndicator {
	step := &stepIndicator{label: widget.NewLabelWithStyle(text, fyne.TextAlignCenter, fyne.TextStyle{})}
	step.ExtendBaseWidget(step)
	return step
}

func (s *stepIndicator) CreateRenderer() fyne.WidgetRenderer {
	background := canvas.NewRectangle(color.Transparent)
	background.CornerRadius = 8
	line := canvas.NewRectangle(color.Transparent)
	line.SetMinSize(fyne.NewSize(0, 3))
	content := container.NewStack(background, container.NewBorder(nil, line, nil, nil, s.label))
	renderer := &stepIndicatorRenderer{step: s, content: content, background: background, line: line}
	renderer.Refresh()
	return renderer
}

type stepIndicatorRenderer struct {
	step       *stepIndicator
	content    *fyne.Container
	background *canvas.Rectangle
	line       *canvas.Rectangle
}

func (r *stepIndicatorRenderer) Layout(size fyne.Size) { r.content.Resize(size) }
func (r *stepIndicatorRenderer) MinSize() fyne.Size    { return r.content.MinSize() }
func (r *stepIndicatorRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.content}
}
func (r *stepIndicatorRenderer) Destroy() {}
func (r *stepIndicatorRenderer) Refresh() {
	r.background.FillColor = color.Transparent
	r.line.FillColor = theme.Color(theme.ColorNameSeparator)
	r.step.label.Importance = widget.LowImportance
	r.step.label.TextStyle.Bold = r.step.active
	if r.step.active {
		primary := theme.Color(theme.ColorNamePrimary)
		red, green, blue, _ := primary.RGBA()
		r.background.FillColor = color.NRGBA{R: uint8(red >> 8), G: uint8(green >> 8), B: uint8(blue >> 8), A: 24}
		r.line.FillColor = primary
		r.step.label.Importance = widget.HighImportance
	}
	r.background.Refresh()
	r.line.Refresh()
	r.step.label.Refresh()
	r.content.Refresh()
}
