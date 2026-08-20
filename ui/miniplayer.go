package ui

import (
	"image"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/lang"
	fynetheme "fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/cenkalti/dominantcolor"

	"github.com/supersonic-app/supersonic/backend"
	"github.com/supersonic-app/supersonic/backend/mediaprovider"
	"github.com/supersonic-app/supersonic/backend/player"
	myTheme "github.com/supersonic-app/supersonic/ui/theme"
	"github.com/supersonic-app/supersonic/ui/util"
	"github.com/supersonic-app/supersonic/ui/widgets"
)

// MiniPlayer is a small always-on-top window with the current track's
// art and basic playback controls, in the style of Spotify's miniplayer.
// The layout is responsive: a tall window shows a card with a large
// cover; a wide, short window shows a compact bar.
type MiniPlayer struct {
	// OnVisibilityChanged is invoked with true when the window is
	// shown and false when it is hidden.
	OnVisibilityChanged func(bool)
	// OnAddToPlaylist is invoked when the add-to-playlist button is tapped.
	OnAddToPlaylist func()

	fyneApp fyne.App
	pm      *backend.PlaybackManager
	im      *backend.ImageManager

	window fyne.Window
	shown  bool

	imageLoader util.ThumbnailLoader
	totalTime   float64

	// layout state, updated by miniPlayerRenderer
	cardMode    bool
	coverPos    fyne.Position
	coverSize   fyne.Size
	hoverCtrls  bool // transport shown due to hover (card mode)

	// widgets, positioned manually by miniPlayerRenderer
	bg        *canvas.Rectangle
	cover     *widgets.ImagePlaceholder
	scrim     *canvas.Rectangle // dims the cover behind hover controls
	title     *widget.Label
	artist    *widget.Label
	addBtn    *widgets.IconButton
	prev      *tapIcon
	playpause *tapIcon
	next      *tapIcon
	seekbar   *widgets.FlatSeekbar
	curTime   *widget.Label
	totalLbl  *widget.Label
}

// tapIcon is a minimal tappable icon. Unlike IconButton it is
// deliberately NOT desktop.Hoverable: hoverable children steal
// mouse-over events from the miniplayer's root widget, which made the
// hover-revealed transport controls flicker (show -> child hover ->
// root MouseOut -> hide -> repeat).
type tapIcon struct {
	widget.BaseWidget
	onTapped func()
	icon     *widget.Icon
	size     fyne.Size
}

var _ fyne.Tappable = (*tapIcon)(nil)

func newTapIcon(res fyne.Resource, size float32, onTapped func()) *tapIcon {
	t := &tapIcon{onTapped: onTapped, icon: widget.NewIcon(res), size: fyne.NewSquareSize(size)}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tapIcon) SetResource(res fyne.Resource) {
	t.icon.SetResource(res)
}

func (t *tapIcon) MinSize() fyne.Size {
	return t.size
}

func (t *tapIcon) Tapped(*fyne.PointEvent) {
	if t.onTapped != nil {
		t.onTapped()
	}
}

func (t *tapIcon) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.icon)
}

func NewMiniPlayer(fyneApp fyne.App, pm *backend.PlaybackManager, im *backend.ImageManager) *MiniPlayer {
	m := &MiniPlayer{fyneApp: fyneApp, pm: pm, im: im}

	m.bg = canvas.NewRectangle(color.Transparent)
	m.bg.CornerRadius = 12

	m.cover = widgets.NewImagePlaceholder(myTheme.TracksIcon, 48)
	m.cover.ScaleMode = canvas.ImageScaleFastest
	m.cover.CornerRadiusOverride = 8

	m.scrim = canvas.NewRectangle(color.Transparent)
	m.scrim.CornerRadius = 12
	m.scrim.Hidden = true

	m.addBtn = widgets.NewIconButton(fynetheme.ContentAddIcon(), func() {
		if m.OnAddToPlaylist != nil {
			m.OnAddToPlaylist()
		}
	})
	m.addBtn.SetToolTip(lang.L("Add to playlist"))
	m.addBtn.Disable()

	m.title = widget.NewLabel("")
	m.title.TextStyle = fyne.TextStyle{Bold: true}
	m.title.SizeName = fynetheme.SizeNameSubHeadingText
	m.title.Truncation = fyne.TextTruncateEllipsis
	m.artist = widget.NewLabel("")
	m.artist.SizeName = myTheme.SizeNameSubText
	m.artist.Truncation = fyne.TextTruncateEllipsis

	m.prev = newTapIcon(fynetheme.MediaSkipPreviousIcon(), 30, func() { pm.SeekBackOrPrevious() })
	m.playpause = newTapIcon(fynetheme.MediaPlayIcon(), 42, func() { pm.PlayPause() })
	m.next = newTapIcon(fynetheme.MediaSkipNextIcon(), 30, func() { pm.SeekNext() })

	m.seekbar = widgets.NewFlatSeekbar()
	m.seekbar.Disable()
	m.seekbar.OnSeeked = func(f float64) { pm.SeekFraction(f) }
	m.seekbar.OnDragging = func(f float64) {
		m.curTime.SetText(util.SecondsToMMSS(f * m.totalTime))
	}

	m.curTime = widget.NewLabel(util.SecondsToMMSS(0))
	m.curTime.SizeName = myTheme.SizeNameSubText
	m.totalLbl = widget.NewLabel(util.SecondsToMMSS(0))
	m.totalLbl.SizeName = myTheme.SizeNameSubText

	m.imageLoader = util.NewThumbnailLoader(im, m.onImageLoaded)
	m.imageLoader.OnBeforeLoad = func() {
		m.cover.SetImage(nil, false)
		m.bg.FillColor = color.Transparent
		m.bg.Refresh()
	}

	// playback state wiring; registered once for the app's lifetime
	pm.OnSongChange(func(item mediaprovider.MediaItem, _ *mediaprovider.Track) {
		fyne.Do(func() { m.onSongChange(item) })
	})
	pm.OnPlayTimeUpdate(func(cur, total float64, _ bool) {
		fyne.Do(func() {
			if !pm.IsSeeking() {
				m.updatePlayTime(cur, total)
			}
		})
	})
	pm.OnPlaying(util.FyneDoFunc(func() { m.setPlaying(true) }))
	pm.OnPaused(util.FyneDoFunc(func() { m.setPlaying(false) }))
	pm.OnStopped(util.FyneDoFunc(func() {
		m.setPlaying(false)
		m.updatePlayTime(0, 0)
	}))

	return m
}

// Canvas returns the miniplayer window's canvas, or nil if the window
// has not been created yet.
func (m *MiniPlayer) Canvas() fyne.Canvas {
	if m.window == nil {
		return nil
	}
	return m.window.Canvas()
}

// Toggle shows the miniplayer window, creating it on first use,
// or hides it if currently shown.
func (m *MiniPlayer) Toggle() {
	if m.window == nil {
		m.createWindow()
	}
	if m.shown {
		m.window.Hide()
		m.setShown(false)
		return
	}
	m.window.Show()
	// best-effort: some window managers (e.g. Wayland) may ignore this
	if dw, ok := m.window.(desktop.Window); ok {
		dw.RequestAlwaysOnTop()
	}
	m.setShown(true)
}

func (m *MiniPlayer) createWindow() {
	m.window = m.fyneApp.NewWindow(lang.L("Miniplayer"))
	m.window.SetContent(newMiniPlayerContent(m))
	m.window.Resize(fyne.NewSize(300, 360))
	m.window.SetCloseIntercept(func() {
		m.window.Hide()
		m.setShown(false)
	})
	// populate current state
	m.onSongChange(m.pm.NowPlaying())
	status := m.pm.PlaybackStatus()
	m.updatePlayTime(status.TimePos, status.Duration)
	m.setPlaying(status.State == player.Playing)
}

func (m *MiniPlayer) setShown(shown bool) {
	m.shown = shown
	if m.OnVisibilityChanged != nil {
		m.OnVisibilityChanged(shown)
	}
}

func (m *MiniPlayer) onSongChange(item mediaprovider.MediaItem) {
	if item == nil {
		m.title.SetText("")
		m.artist.SetText("")
		m.imageLoader.Load("")
		return
	}
	meta := item.Metadata()
	m.title.SetText(meta.Name)
	if tr, ok := item.(*mediaprovider.Track); ok {
		m.artist.SetText(strings.Join(tr.ArtistNames, ", "))
		m.addBtn.Enable()
	} else {
		m.artist.SetText("")
		m.addBtn.Disable()
	}
	m.imageLoader.Load(meta.CoverArtID)
}

// setHoverControls shows or hides the transport controls overlaid on
// the cover in card mode. In bar mode the controls are always shown.
func (m *MiniPlayer) setHoverControls(show bool) {
	if m.hoverCtrls == show {
		return
	}
	m.hoverCtrls = show
	m.applyControlVisibility()
	m.prev.Refresh()
	m.playpause.Refresh()
	m.next.Refresh()
	m.scrim.Refresh()
}

func (m *MiniPlayer) applyControlVisibility() {
	showCtrls := !m.cardMode || m.hoverCtrls
	m.prev.Hidden = !showCtrls
	m.playpause.Hidden = !showCtrls
	m.next.Hidden = !showCtrls
	showScrim := m.cardMode && m.hoverCtrls
	if showScrim {
		// dim the artwork toward the theme background so the
		// foreground-colored buttons stay legible on any cover
		th := fyne.CurrentApp().Settings().Theme()
		v := fyne.CurrentApp().Settings().ThemeVariant()
		bg := th.Color(fynetheme.ColorNameBackground, v)
		cr, cg, cb, _ := bg.RGBA()
		m.scrim.FillColor = color.NRGBA{R: uint8(cr >> 8), G: uint8(cg >> 8), B: uint8(cb >> 8), A: 170}
	}
	m.scrim.Hidden = !showScrim
}

func (m *MiniPlayer) onImageLoaded(img image.Image) {
	m.cover.SetImage(img, false)
	if img != nil {
		c := dominantcolor.Find(img)
		m.bg.FillColor = color.NRGBA{R: c.R, G: c.G, B: c.B, A: 210}
	} else {
		m.bg.FillColor = color.Transparent
	}
	m.bg.Refresh()
}

func (m *MiniPlayer) updatePlayTime(cur, total float64) {
	m.totalTime = total
	if total > 0 {
		m.seekbar.Enable()
	} else {
		m.seekbar.Disable()
	}
	tt := util.SecondsToMMSS(total)
	if tt != m.totalLbl.Text {
		m.totalLbl.SetText(tt)
	}
	if !m.seekbar.IsDragging() {
		ct := util.SecondsToMMSS(cur)
		if ct != m.curTime.Text {
			m.curTime.SetText(ct)
		}
		v := 0.0
		if total > 0 {
			v = cur / total
		}
		m.seekbar.SetProgress(v)
	}
}

func (m *MiniPlayer) setPlaying(playing bool) {
	if playing {
		m.playpause.SetResource(fynetheme.MediaPauseIcon())
	} else {
		m.playpause.SetResource(fynetheme.MediaPlayIcon())
	}
}

// miniPlayerContent is the responsive root widget of the miniplayer
// window: a tall aspect shows the card layout, a wide one the bar layout.
// It tracks the mouse to reveal the transport controls when hovering
// over the cover in card mode.
type miniPlayerContent struct {
	widget.BaseWidget
	mp *MiniPlayer
}

func newMiniPlayerContent(mp *MiniPlayer) *miniPlayerContent {
	c := &miniPlayerContent{mp: mp}
	c.ExtendBaseWidget(c)
	return c
}

func (c *miniPlayerContent) CreateRenderer() fyne.WidgetRenderer {
	return &miniPlayerRenderer{mp: c.mp}
}

var _ desktop.Hoverable = (*miniPlayerContent)(nil)

func (c *miniPlayerContent) MouseIn(e *desktop.MouseEvent) {
	c.MouseMoved(e)
}

func (c *miniPlayerContent) MouseMoved(e *desktop.MouseEvent) {
	m := c.mp
	if !m.cardMode {
		return
	}
	in := e.Position.X >= m.coverPos.X && e.Position.X <= m.coverPos.X+m.coverSize.Width &&
		e.Position.Y >= m.coverPos.Y && e.Position.Y <= m.coverPos.Y+m.coverSize.Height
	m.setHoverControls(in)
}

func (c *miniPlayerContent) MouseOut() {
	c.mp.setHoverControls(false)
}

type miniPlayerRenderer struct {
	mp *MiniPlayer
}

func (r *miniPlayerRenderer) Objects() []fyne.CanvasObject {
	m := r.mp
	return []fyne.CanvasObject{m.bg, m.cover, m.scrim,
		m.prev, m.playpause, m.next,
		m.title, m.artist, m.addBtn, m.curTime, m.seekbar, m.totalLbl}
}

func (r *miniPlayerRenderer) MinSize() fyne.Size {
	return fyne.NewSize(240, 100)
}

func (r *miniPlayerRenderer) Destroy() {}

func (r *miniPlayerRenderer) Refresh() {
	for _, o := range r.Objects() {
		o.Refresh()
	}
}

func (r *miniPlayerRenderer) Layout(size fyne.Size) {
	m := r.mp
	// square-ish and portrait windows use the card layout;
	// only clearly wide windows use the bar
	cardMode := size.Height >= size.Width*0.75
	if cardMode != m.cardMode {
		m.cardMode = cardMode
		if !cardMode {
			m.hoverCtrls = false
		}
		m.applyControlVisibility()
	}
	if cardMode {
		r.layoutCard(size)
	} else {
		r.layoutBar(size)
	}
}

// layoutCard: cover nearly filling the window with small margins,
// transport controls overlaid on hover, a slim seek line below,
// then title/artist with the add-to-playlist button on the right.
func (r *miniPlayerRenderer) layoutCard(size fyne.Size) {
	m := r.mp
	pad := float32(5) // slim margin between the card and window edges

	titleH := m.title.MinSize().Height
	artistH := m.artist.MinSize().Height
	textH := titleH + artistH - 14
	textY := size.Height - textH - 2

	seekH := float32(18)
	seekY := textY - seekH + 4

	// cover card: everything above the seek line, small margins
	regW := size.Width - 2*pad
	regH := seekY - 2 - pad
	if regH < 0 {
		regH = 0
	}
	m.bg.Move(fyne.NewPos(pad, pad))
	m.bg.Resize(fyne.NewSize(regW, regH))
	// generous inset so the tinted card reads as a border around the art
	coverSize := fyne.Min(regW, regH) - 28
	if coverSize < 0 {
		coverSize = 0
	}
	m.cover.Move(fyne.NewPos(pad+(regW-coverSize)/2, pad+(regH-coverSize)/2))
	m.cover.Resize(fyne.NewSize(coverSize, coverSize))
	// hover region and scrim cover the whole card
	m.coverPos = fyne.NewPos(pad, pad)
	m.coverSize = fyne.NewSize(regW, regH)
	m.scrim.Move(m.coverPos)
	m.scrim.Resize(m.coverSize)

	// transport overlay centered on the cover
	ctrlH := m.playpause.MinSize().Height
	bw := m.prev.MinSize().Width + m.playpause.MinSize().Width + m.next.MinSize().Width + 16
	r.placeTransport((size.Width-bw)/2, pad+(regH-ctrlH)/2, ctrlH)

	// slim seek line, full width; time labels are hidden in card mode
	m.curTime.Hide()
	m.totalLbl.Hide()
	m.seekbar.Move(fyne.NewPos(pad+2, seekY))
	m.seekbar.Resize(fyne.NewSize(size.Width-2*pad-4, seekH))

	// title/artist left, add-to-playlist right
	m.addBtn.Show()
	addSz := m.addBtn.MinSize()
	textW := size.Width - 2*pad - addSz.Width - 10
	m.title.Move(fyne.NewPos(pad-4, textY))
	m.title.Resize(fyne.NewSize(textW+4, titleH))
	m.artist.Move(fyne.NewPos(pad-4, textY+titleH-14))
	m.artist.Resize(fyne.NewSize(textW+4, artistH))
	m.addBtn.Move(fyne.NewPos(size.Width-pad-addSz.Width, textY+(textH-addSz.Height)/2))
	m.addBtn.Resize(addSz)
}

// layoutBar: cover on the left, text and controls to the right.
func (r *miniPlayerRenderer) layoutBar(size fyne.Size) {
	m := r.mp
	pad := float32(8)

	// bar mode has no hover overlay or add button, and shows the times
	m.curTime.Show()
	m.totalLbl.Show()
	m.addBtn.Hide()
	m.scrim.Move(fyne.NewPos(0, 0))
	m.scrim.Resize(fyne.NewSize(0, 0))

	coverSize := size.Height - 2*pad
	m.bg.Move(fyne.NewPos(pad, pad))
	m.bg.Resize(fyne.NewSize(coverSize, coverSize))
	m.cover.Move(fyne.NewPos(pad+3, pad+3))
	m.cover.Resize(fyne.NewSize(coverSize-6, coverSize-6))

	tx := pad + coverSize + 10
	tw := size.Width - tx - pad
	titleH := m.title.MinSize().Height
	artistH := m.artist.MinSize().Height

	m.title.Move(fyne.NewPos(tx-4, 0))
	m.title.Resize(fyne.NewSize(tw+4, titleH))
	m.artist.Move(fyne.NewPos(tx-4, titleH-12))
	m.artist.Resize(fyne.NewSize(tw+4, artistH))

	rowH := float32(30)
	rowY := size.Height - rowH - 4
	bw := r.placeTransport(tx, rowY, rowH)
	r.placeSeekRow(tx+bw+8, rowY, size.Width-(tx+bw+8)-pad, rowH)
}

// placeTransport lays out prev/play/next starting at x, vertically
// centered in a row of height h at y; returns the total width used.
func (r *miniPlayerRenderer) placeTransport(x, y, h float32) float32 {
	m := r.mp
	startX := x
	for _, b := range []fyne.CanvasObject{m.prev, m.playpause, m.next} {
		ms := b.MinSize()
		b.Resize(ms)
		b.Move(fyne.NewPos(x, y+(h-ms.Height)/2))
		x += ms.Width + 8
	}
	return x - startX - 8
}

func (r *miniPlayerRenderer) placeSeekRow(x, y, w, h float32) {
	m := r.mp
	ctW := m.curTime.MinSize().Width
	ttW := m.totalLbl.MinSize().Width
	m.curTime.Move(fyne.NewPos(x-4, y+(h-m.curTime.MinSize().Height)/2))
	m.curTime.Resize(fyne.NewSize(ctW, m.curTime.MinSize().Height))
	m.totalLbl.Move(fyne.NewPos(x+w-ttW+4, y+(h-m.totalLbl.MinSize().Height)/2))
	m.totalLbl.Resize(fyne.NewSize(ttW, m.totalLbl.MinSize().Height))
	sx := x + ctW - 2
	sw := w - ctW - ttW + 4
	if sw < 0 {
		sw = 0
	}
	m.seekbar.Move(fyne.NewPos(sx, y))
	m.seekbar.Resize(fyne.NewSize(sw, h))
}
