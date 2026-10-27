package ui

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const purgeHoldDuration = 3 * time.Second

type holdButton struct {
	widget.Button
	started   time.Time
	keyboard  bool
	animation *fyne.Animation
	timer     *time.Timer
	progress  float32
	focused   bool
	onHeld    func()
}

func newHoldButton(onHeld func()) *holdButton {
	button := &holdButton{onHeld: onHeld}
	button.Text = "Hold to Confirm"
	button.Importance = widget.DangerImportance
	button.ExtendBaseWidget(button)
	return button
}

func (b *holdButton) start(keyboard bool) {
	if b.Disabled() || !b.started.IsZero() {
		return
	}
	b.started = time.Now()
	b.keyboard = keyboard
	started := b.started
	b.animation = fyne.NewAnimation(purgeHoldDuration, func(value float32) {
		fyne.Do(func() {
			if b.started == started {
				b.setProgress(value)
			}
		})
	})
	b.animation.Curve = fyne.AnimationLinear
	b.animation.Start()
	b.timer = time.AfterFunc(purgeHoldDuration, func() {
		fyne.Do(func() { b.complete(started) })
	})
}

func (b *holdButton) cancel() {
	b.started = time.Time{}
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	if b.animation != nil {
		b.animation.Stop()
		b.animation = nil
	}
	b.setProgress(0)
}

func (b *holdButton) setProgress(value float32) {
	b.progress = value
	b.Refresh()
}

func (b *holdButton) release(keyboard bool) {
	if b.started.IsZero() || b.keyboard != keyboard {
		return
	}
	b.cancel()
}

func (b *holdButton) complete(started time.Time) {
	if started.IsZero() || b.started != started || time.Since(started) < purgeHoldDuration {
		return
	}
	if b.Disabled() {
		b.cancel()
		return
	}
	b.cancel()
	b.setProgress(1)
	b.onHeld()
}

func (b *holdButton) MouseDown(event *desktop.MouseEvent) {
	if event.Button == desktop.MouseButtonPrimary && !b.Disabled() {
		fyne.CurrentApp().Driver().CanvasForObject(b).Focus(b)
		b.start(false)
	}
}

func (b *holdButton) MouseUp(event *desktop.MouseEvent) {
	if event.Button == desktop.MouseButtonPrimary {
		b.release(false)
	}
}

func (b *holdButton) MouseOut() {
	b.cancel()
	b.Button.MouseOut()
}

func (b *holdButton) FocusLost() {
	b.focused = false
	b.cancel()
	b.Button.FocusLost()
}

func (b *holdButton) FocusGained() {
	b.focused = true
	b.Button.FocusGained()
}

func (b *holdButton) KeyDown(event *fyne.KeyEvent) {
	if event.Name == fyne.KeySpace {
		b.start(true)
	}
}

func (b *holdButton) KeyUp(event *fyne.KeyEvent) {
	if event.Name == fyne.KeySpace {
		b.release(true)
	}
}

func (b *holdButton) TypedKey(*fyne.KeyEvent) {}

func (b *holdButton) CreateRenderer() fyne.WidgetRenderer {
	renderer := &holdButtonRenderer{
		button:     b,
		background: canvas.NewRectangle(color.Transparent),
		fill:       canvas.NewRectangle(color.Transparent),
		label:      canvas.NewText(b.Text, color.Transparent),
	}
	renderer.label.Alignment = fyne.TextAlignCenter
	renderer.label.TextStyle.Bold = true
	renderer.Refresh()
	return renderer
}

type holdButtonRenderer struct {
	button     *holdButton
	background *canvas.Rectangle
	fill       *canvas.Rectangle
	label      *canvas.Text
}

func (r *holdButtonRenderer) Destroy() {}

func (r *holdButtonRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.background, r.fill, r.label}
}

func (r *holdButtonRenderer) MinSize() fyne.Size {
	return r.label.MinSize().Add(fyne.NewSize(48, 24)).Max(fyne.NewSize(190, 44))
}

func (r *holdButtonRenderer) Layout(size fyne.Size) {
	r.background.Resize(size)
	r.fill.Resize(fyne.NewSize(size.Width*r.button.progress, size.Height))
	r.fill.CornerRadius = fyne.Min(r.background.CornerRadius, r.fill.Size().Width/2)
	r.label.Resize(fyne.NewSize(size.Width, r.label.MinSize().Height))
	r.label.Move(fyne.NewPos(0, (size.Height-r.label.MinSize().Height)/2))
}

func (r *holdButtonRenderer) Refresh() {
	th := r.button.Theme()
	variant := fyne.CurrentApp().Settings().ThemeVariant()
	r.background.FillColor = th.Color(theme.ColorNameButton, variant)
	r.background.CornerRadius = th.Size(theme.SizeNameButtonRadius)
	r.background.StrokeWidth = 0
	if r.button.focused {
		r.background.StrokeWidth = 2
		r.background.StrokeColor = th.Color(theme.ColorNamePrimary, variant)
	}
	fill := color.NRGBAModel.Convert(th.Color(theme.ColorNamePrimary, variant)).(color.NRGBA)
	fill.A = 90
	r.fill.FillColor = fill
	r.label.Text = r.button.Text
	r.label.TextSize = th.Size(theme.SizeNameText)
	r.label.Color = th.Color(theme.ColorNameForeground, variant)
	if r.button.Disabled() {
		r.label.Color = th.Color(theme.ColorNameDisabled, variant)
	}
	r.Layout(r.button.Size())
	r.background.Refresh()
	r.fill.Refresh()
	r.label.Refresh()
}
