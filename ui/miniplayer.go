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

	fyneApp fyne.App
	pm      *backend.PlaybackManager
	im      *backend.ImageManager

	window fyne.Window
	shown  bool

	imageLoader util.ThumbnailLoader
	totalTime   float64

	// widgets, positioned manually by miniPlayerRenderer
	bg        *canvas.Rectangle
	cover     *widgets.ImagePlaceholder
	title     *widget.Label
	artist    *widget.Label
	prev      *widgets.IconButton
	playpause *widgets.IconButton
	next      *widgets.IconButton
	seekbar   *widgets.FlatSeekbar
	curTime   *widget.Label
	totalLbl  *widget.Label
}

func NewMiniPlayer(fyneApp fyne.App, pm *backend.PlaybackManager, im *backend.ImageManager) *MiniPlayer {
	m := &MiniPlayer{fyneApp: fyneApp, pm: pm, im: im}

	m.bg = canvas.NewRectangle(color.Transparent)
	m.bg.CornerRadius = 12

	m.cover = widgets.NewImagePlaceholder(myTheme.TracksIcon, 48)
	m.cover.ScaleMode = canvas.ImageScaleFastest

	m.title = widget.NewLabel("")
	m.title.TextStyle = fyne.TextStyle{Bold: true}
	m.title.Truncation = fyne.TextTruncateEllipsis
	m.artist = widget.NewLabel("")
	m.artist.SizeName = myTheme.SizeNameSubText
	m.artist.Truncation = fyne.TextTruncateEllipsis

	m.prev = widgets.NewIconButton(fynetheme.MediaSkipPreviousIcon(), func() { pm.SeekBackOrPrevious() })
	m.prev.SetToolTip(lang.L("Previous"))
	m.playpause = widgets.NewIconButton(fynetheme.MediaPlayIcon(), func() { pm.PlayPause() })
	m.playpause.IconSize = widgets.IconButtonSizeBigger
	m.playpause.SetToolTip(lang.L("Play"))
	m.next = widgets.NewIconButton(fynetheme.MediaSkipNextIcon(), func() { pm.SeekNext() })
	m.next.SetToolTip(lang.L("Next"))

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
	} else {
		m.artist.SetText("")
	}
	m.imageLoader.Load(meta.CoverArtID)
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
		m.playpause.SetIcon(fynetheme.MediaPauseIcon())
		m.playpause.SetToolTip(lang.L("Pause"))
	} else {
		m.playpause.SetIcon(fynetheme.MediaPlayIcon())
		m.playpause.SetToolTip(lang.L("Play"))
	}
}

// miniPlayerContent is the responsive root widget of the miniplayer
// window: a tall aspect shows the card layout, a wide one the bar layout.
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

type miniPlayerRenderer struct {
	mp *MiniPlayer
}

func (r *miniPlayerRenderer) Objects() []fyne.CanvasObject {
	m := r.mp
	return []fyne.CanvasObject{m.bg, m.cover, m.title, m.artist,
		m.prev, m.playpause, m.next, m.curTime, m.seekbar, m.totalLbl}
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
	if size.Height >= size.Width*0.9 {
		r.layoutCard(size)
	} else {
		r.layoutBar(size)
	}
}

// layoutCard: large tinted cover card on top, then title, artist,
// transport row, and the seek row at the bottom.
func (r *miniPlayerRenderer) layoutCard(size fyne.Size) {
	m := r.mp
	pad := float32(10)

	seekRowH := float32(28)
	ctrlH := m.playpause.MinSize().Height
	titleH := m.title.MinSize().Height
	artistH := m.artist.MinSize().Height

	seekY := size.Height - seekRowH - 2
	ctrlY := seekY - ctrlH + 2
	artistY := ctrlY - artistH + 8
	titleY := artistY - titleH + 12

	// tinted card with the cover centered inside
	bgH := titleY - pad
	m.bg.Move(fyne.NewPos(pad, pad))
	m.bg.Resize(fyne.NewSize(size.Width-2*pad, bgH-pad))
	coverSize := fyne.Min(bgH-3*pad, size.Width-4*pad)
	if coverSize < 0 {
		coverSize = 0
	}
	m.cover.Move(fyne.NewPos((size.Width-coverSize)/2, pad+(bgH-pad-coverSize)/2))
	m.cover.Resize(fyne.NewSize(coverSize, coverSize))

	m.title.Move(fyne.NewPos(pad-4, titleY))
	m.title.Resize(fyne.NewSize(size.Width-2*pad+8, titleH))
	m.artist.Move(fyne.NewPos(pad-4, artistY))
	m.artist.Resize(fyne.NewSize(size.Width-2*pad+8, artistH))

	// transport centered
	bw := m.prev.MinSize().Width + m.playpause.MinSize().Width + m.next.MinSize().Width + 16
	x := (size.Width - bw) / 2
	r.placeTransport(x, ctrlY, ctrlH)

	r.placeSeekRow(pad, seekY, size.Width-2*pad, seekRowH)
}

// layoutBar: cover on the left, text and controls to the right.
func (r *miniPlayerRenderer) layoutBar(size fyne.Size) {
	m := r.mp
	pad := float32(8)

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
