package widgets

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	flatSeekbarLineHeight = 4
	flatSeekbarMinHeight  = 24
)

// FlatSeekbar is a minimal two-tone seek bar: a straight line with no
// thumb circle, Spotify-style. The played portion uses the theme
// foreground color (primary while hovered, focused or dragging), and
// the remaining portion uses the input background color so it stays
// visible against the panel background. All colors are theme-driven.
type FlatSeekbar struct {
	widget.DisableableWidget

	// OnSeeked is called when the user seeks by tapping, releasing a
	// drag, or with the arrow keys; the argument is the position (0-1).
	OnSeeked func(float64)
	// OnDragging is called continuously during a drag,
	// for previewing the seek position in a time label.
	OnDragging func(float64)

	progress float64
	dragging bool
	hovered  bool
	focused  bool

	played    *canvas.Rectangle
	remaining *canvas.Rectangle
}

var (
	_ fyne.Tappable      = (*FlatSeekbar)(nil)
	_ fyne.Draggable     = (*FlatSeekbar)(nil)
	_ fyne.Focusable     = (*FlatSeekbar)(nil)
	_ fyne.Disableable   = (*FlatSeekbar)(nil)
	_ desktop.Hoverable  = (*FlatSeekbar)(nil)
	_ desktop.Cursorable = (*FlatSeekbar)(nil)
)

func NewFlatSeekbar() *FlatSeekbar {
	f := &FlatSeekbar{
		played:    canvas.NewRectangle(nil),
		remaining: canvas.NewRectangle(nil),
	}
	f.played.CornerRadius = flatSeekbarLineHeight / 2
	f.remaining.CornerRadius = flatSeekbarLineHeight / 2
	f.ExtendBaseWidget(f)
	return f
}

// SetProgress sets the played ratio (0 to 1) shown by the bar.
// Ignored while the user is dragging.
func (f *FlatSeekbar) SetProgress(v float64) {
	if f.dragging {
		return
	}
	f.setProgress(v)
}

func (f *FlatSeekbar) IsDragging() bool {
	return f.dragging
}

func (f *FlatSeekbar) Disable() {
	f.DisableableWidget.Disable()
	f.Refresh()
}

func (f *FlatSeekbar) Enable() {
	f.DisableableWidget.Enable()
	f.Refresh()
}

func (f *FlatSeekbar) Tapped(e *fyne.PointEvent) {
	if f.Disabled() {
		return
	}
	p := f.posToProgress(e.Position.X)
	f.setProgress(p)
	if f.OnSeeked != nil {
		f.OnSeeked(p)
	}
	// don't keep focus after being tapped
	if c := fyne.CurrentApp().Driver().CanvasForObject(f); c != nil {
		c.Unfocus()
	}
}

func (f *FlatSeekbar) Dragged(e *fyne.DragEvent) {
	if f.Disabled() {
		return
	}
	f.dragging = true
	f.setProgress(f.posToProgress(e.Position.X))
	if f.OnDragging != nil {
		f.OnDragging(f.progress)
	}
}

func (f *FlatSeekbar) DragEnd() {
	if !f.dragging {
		return
	}
	f.dragging = false
	if f.OnSeeked != nil {
		f.OnSeeked(f.progress)
	}
	f.Refresh()
}

func (f *FlatSeekbar) MouseIn(e *desktop.MouseEvent) {
	if f.Disabled() {
		return
	}
	f.hovered = true
	f.Refresh()
}

func (f *FlatSeekbar) MouseMoved(e *desktop.MouseEvent) {
}

func (f *FlatSeekbar) MouseOut() {
	f.hovered = false
	f.Refresh()
}

func (f *FlatSeekbar) Cursor() desktop.Cursor {
	if f.hovered && !f.Disabled() {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

func (f *FlatSeekbar) FocusGained() {
	f.focused = true
	f.Refresh()
}

func (f *FlatSeekbar) FocusLost() {
	f.focused = false
	f.Refresh()
}

func (f *FlatSeekbar) TypedKey(e *fyne.KeyEvent) {
	p := f.progress
	switch e.Name {
	case fyne.KeyLeft:
		p = max(p-0.05, 0)
	case fyne.KeyRight:
		p = min(p+0.05, 1)
	default:
		return
	}
	f.setProgress(p)
	if f.OnSeeked != nil {
		f.OnSeeked(p)
	}
}

func (f *FlatSeekbar) TypedRune(r rune) {
}

func (f *FlatSeekbar) CreateRenderer() fyne.WidgetRenderer {
	return &flatSeekbarRenderer{f: f}
}

func (f *FlatSeekbar) setProgress(v float64) {
	v = max(0, min(1, v))
	if v == f.progress {
		return
	}
	f.progress = v
	f.Refresh()
}

func (f *FlatSeekbar) posToProgress(x float32) float64 {
	w := f.Size().Width
	if w <= 0 {
		return 0
	}
	return float64(x / w)
}

func (f *FlatSeekbar) applyColors() {
	th := f.Theme()
	v := fyne.CurrentApp().Settings().ThemeVariant()
	playedColor := th.Color(theme.ColorNameForeground, v)
	if f.Disabled() {
		playedColor = th.Color(theme.ColorNameDisabled, v)
	} else if f.hovered || f.focused || f.dragging {
		playedColor = th.Color(theme.ColorNamePrimary, v)
	}
	f.played.FillColor = playedColor
	f.remaining.FillColor = th.Color(theme.ColorNameInputBackground, v)
}

func (f *FlatSeekbar) layoutBars(size fyne.Size) {
	y := (size.Height - flatSeekbarLineHeight) / 2
	w := size.Width * float32(f.progress)
	f.played.Move(fyne.NewPos(0, y))
	f.played.Resize(fyne.NewSize(w, flatSeekbarLineHeight))
	f.remaining.Move(fyne.NewPos(w, y))
	f.remaining.Resize(fyne.NewSize(size.Width-w, flatSeekbarLineHeight))
}

type flatSeekbarRenderer struct {
	f *FlatSeekbar
}

func (r *flatSeekbarRenderer) Layout(size fyne.Size) {
	r.f.layoutBars(size)
}

func (r *flatSeekbarRenderer) MinSize() fyne.Size {
	return fyne.NewSize(50, flatSeekbarMinHeight)
}

func (r *flatSeekbarRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.f.remaining, r.f.played}
}

func (r *flatSeekbarRenderer) Refresh() {
	r.f.applyColors()
	r.f.layoutBars(r.f.Size())
	r.f.played.Refresh()
	r.f.remaining.Refresh()
}

func (r *flatSeekbarRenderer) Destroy() {
}
