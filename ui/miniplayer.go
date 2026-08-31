package ui

import (
	"image"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
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
	// OnSetFavorite is invoked when the favorite button toggles.
	OnSetFavorite func(favorite bool)

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
	favBtn    *widgets.IconButton
	favorited bool
	addBtn    *widgets.IconButton
	prev      *tapIcon
	playpause *tapIcon
	next      *tapIcon
	shuffle   *tapIcon // bar mode only
	loop      *tapIcon // bar mode only
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
	bg       *canvas.Rectangle
	size     fyne.Size
	hovered  bool
	active   bool
}

var _ fyne.Tappable = (*tapIcon)(nil)

func newTapIcon(res fyne.Resource, size float32, onTapped func()) *tapIcon {
	t := &tapIcon{onTapped: onTapped, icon: widget.NewIcon(res), size: fyne.NewSquareSize(size)}
	t.bg = canvas.NewRectangle(color.Transparent)
	t.bg.CornerRadius = size / 2
	t.ExtendBaseWidget(t)
	return t
}

func (t *tapIcon) SetResource(res fyne.Resource) {
	t.icon.SetResource(res)
}

// SetHovered applies the theme hover color behind the icon. Hover
// tracking is done by the miniplayer root widget, since making this
// widget Hoverable would steal events from it (see type comment).
func (t *tapIcon) SetHovered(hovered bool) {
	if t.hovered == hovered {
		return
	}
	t.hovered = hovered
	t.recolor()
}

// SetActive marks a toggle icon (shuffle, loop) as engaged,
// tinting its background with the theme selection color.
func (t *tapIcon) SetActive(active bool) {
	if t.active == active {
		return
	}
	t.active = active
	t.recolor()
}

func (t *tapIcon) recolor() {
	th := fyne.CurrentApp().Settings().Theme()
	v := fyne.CurrentApp().Settings().ThemeVariant()
	switch {
	case t.hovered:
		t.bg.FillColor = th.Color(fynetheme.ColorNameHover, v)
	case t.active:
		t.bg.FillColor = th.Color(fynetheme.ColorNameSelection, v)
	default:
		t.bg.FillColor = color.Transparent
	}
	t.bg.Refresh()
}

func (t *tapIcon) MinSize() fyne.Size {
	return t.size
}

// setIconSize adjusts the icon's footprint; used by the responsive
// layouts (bigger in the card overlay, compact in the bar).
func (t *tapIcon) setIconSize(size float32) {
	if t.size.Width == size {
		return
	}
	t.size = fyne.NewSquareSize(size)
	t.bg.CornerRadius = size / 2
}

func (t *tapIcon) Tapped(*fyne.PointEvent) {
	if t.onTapped != nil {
		t.onTapped()
	}
}

func (t *tapIcon) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(t.bg, t.icon))
}

func NewMiniPlayer(fyneApp fyne.App, pm *backend.PlaybackManager, im *backend.ImageManager) *MiniPlayer {
	m := &MiniPlayer{fyneApp: fyneApp, pm: pm, im: im}

	m.bg = canvas.NewRectangle(color.Transparent)
	m.bg.CornerRadius = 12

	m.cover = widgets.NewImagePlaceholder(myTheme.TracksIcon, 48)
	m.cover.ScaleMode = canvas.ImageScaleFastest
	m.cover.CornerRadiusOverride = 8
	// stretch the art to fill its square rather than letterboxing
	m.cover.SetImageFillMode(canvas.ImageFillStretch)

	m.scrim = canvas.NewRectangle(color.Transparent)
	m.scrim.CornerRadius = 12
	m.scrim.Hidden = true

	m.favBtn = widgets.NewIconButton(myTheme.NotFavoriteIcon, func() {
		m.favorited = !m.favorited
		m.applyFavoriteIcon()
		if m.OnSetFavorite != nil {
			m.OnSetFavorite(m.favorited)
		}
	})
	m.favBtn.SetToolTip(lang.L("Set favorite"))
	m.favBtn.Disable()

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
	m.shuffle = newTapIcon(myTheme.ShuffleIcon, 28, func() { pm.SetShuffle(!pm.IsShuffle()) })
	m.loop = newTapIcon(myTheme.RepeatIcon, 28, func() { pm.SetNextLoopMode() })

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
	pm.OnShuffleChange(func(shuffle bool) {
		fyne.Do(func() { m.shuffle.SetActive(shuffle) })
	})
	pm.OnLoopModeChange(func(mode backend.LoopMode) {
		fyne.Do(func() { m.applyLoopMode(mode) })
	})

	return m
}

func (m *MiniPlayer) applyLoopMode(mode backend.LoopMode) {
	switch mode {
	case backend.LoopOne:
		m.loop.SetResource(myTheme.RepeatOneIcon)
		m.loop.SetActive(true)
	case backend.LoopAll:
		m.loop.SetResource(myTheme.RepeatIcon)
		m.loop.SetActive(true)
	default:
		m.loop.SetResource(myTheme.RepeatIcon)
		m.loop.SetActive(false)
	}
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
	m.shuffle.SetActive(m.pm.IsShuffle())
	m.applyLoopMode(m.pm.GetLoopMode())
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
		m.favorited = tr.Favorite
		m.applyFavoriteIcon()
		m.favBtn.Enable()
	} else {
		m.artist.SetText("")
		m.addBtn.Disable()
		m.favorited = false
		m.applyFavoriteIcon()
		m.favBtn.Disable()
	}
	m.imageLoader.Load(meta.CoverArtID)
}

func (m *MiniPlayer) applyFavoriteIcon() {
	if m.favorited {
		m.favBtn.SetIcon(myTheme.FavoriteIcon)
		m.favBtn.SetToolTip(lang.L("Unset favorite"))
	} else {
		m.favBtn.SetIcon(myTheme.NotFavoriteIcon)
		m.favBtn.SetToolTip(lang.L("Set favorite"))
	}
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
	if m.cardMode {
		in := e.Position.X >= m.coverPos.X && e.Position.X <= m.coverPos.X+m.coverSize.Width &&
			e.Position.Y >= m.coverPos.Y && e.Position.Y <= m.coverPos.Y+m.coverSize.Height
		m.setHoverControls(in)
	}
	// per-icon hover highlight, tracked here since the icons themselves
	// are deliberately not Hoverable
	for _, t := range []*tapIcon{m.prev, m.playpause, m.next, m.shuffle, m.loop} {
		if t.Hidden {
			t.SetHovered(false)
			continue
		}
		pos, sz := t.Position(), t.Size()
		t.SetHovered(e.Position.X >= pos.X && e.Position.X <= pos.X+sz.Width &&
			e.Position.Y >= pos.Y && e.Position.Y <= pos.Y+sz.Height)
	}
}

func (c *miniPlayerContent) MouseOut() {
	m := c.mp
	m.setHoverControls(false)
	for _, t := range []*tapIcon{m.prev, m.playpause, m.next, m.shuffle, m.loop} {
		t.SetHovered(false)
	}
}

type miniPlayerRenderer struct {
	mp *MiniPlayer
}

func (r *miniPlayerRenderer) Objects() []fyne.CanvasObject {
	m := r.mp
	return []fyne.CanvasObject{m.bg, m.cover, m.scrim,
		m.prev, m.playpause, m.next, m.shuffle, m.loop,
		m.title, m.artist, m.favBtn, m.addBtn, m.curTime, m.seekbar, m.totalLbl}
}

func (r *miniPlayerRenderer) MinSize() fyne.Size {
	return fyne.NewSize(220, 56)
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

	// card mode: seek line shown, larger title, no shuffle/loop toggles
	m.seekbar.Show()
	m.shuffle.Hide()
	m.loop.Hide()
	m.prev.setIconSize(30)
	m.playpause.setIconSize(42)
	m.next.setIconSize(30)
	if m.title.SizeName != fynetheme.SizeNameSubHeadingText {
		m.title.SizeName = fynetheme.SizeNameSubHeadingText
		m.title.Refresh()
	}

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
	coverSize := fyne.Min(regW, regH)
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

	// title/artist left; favorite then add-to-playlist on the right
	m.favBtn.Show()
	m.addBtn.Show()
	m.favBtn.IconSize = widgets.IconButtonSizeNormal
	m.addBtn.IconSize = widgets.IconButtonSizeNormal
	favSz := m.favBtn.MinSize()
	addSz := m.addBtn.MinSize()
	btnsW := favSz.Width + 4 + addSz.Width
	textW := size.Width - 2*pad - btnsW - 10
	m.title.Move(fyne.NewPos(pad-4, textY))
	m.title.Resize(fyne.NewSize(textW+4, titleH))
	m.artist.Move(fyne.NewPos(pad-4, textY+titleH-14))
	m.artist.Resize(fyne.NewSize(textW+4, artistH))
	m.favBtn.Move(fyne.NewPos(size.Width-pad-btnsW, textY+(textH-favSz.Height)/2))
	m.favBtn.Resize(favSz)
	m.addBtn.Move(fyne.NewPos(size.Width-pad-addSz.Width, textY+(textH-addSz.Height)/2))
	m.addBtn.Resize(addSz)
}

// layoutBar: one compact Spotify-style row -
// [cover] [title/artist] ... [fav][add]  [shuffle][prev][play][next][loop]
// No seek bar or time labels in this mode.
func (r *miniPlayerRenderer) layoutBar(size fyne.Size) {
	m := r.mp
	pad := float32(8)

	m.curTime.Hide()
	m.totalLbl.Hide()
	m.seekbar.Hide()
	m.favBtn.Show()
	m.addBtn.Show()
	m.shuffle.Show()
	m.loop.Show()
	m.scrim.Move(fyne.NewPos(0, 0))
	m.scrim.Resize(fyne.NewSize(0, 0))
	// compact icons in bar mode
	m.prev.setIconSize(20)
	m.playpause.setIconSize(28)
	m.next.setIconSize(20)
	m.shuffle.setIconSize(18)
	m.loop.setIconSize(18)
	m.favBtn.IconSize = widgets.IconButtonSizeSmaller
	m.addBtn.IconSize = widgets.IconButtonSizeSmaller
	if m.title.SizeName != fynetheme.SizeNameText {
		m.title.SizeName = fynetheme.SizeNameText
		m.title.Refresh()
	}

	coverSize := size.Height - 2*pad
	m.bg.Move(fyne.NewPos(pad, pad))
	m.bg.Resize(fyne.NewSize(coverSize, coverSize))
	m.cover.Move(fyne.NewPos(pad+2, pad+2))
	m.cover.Resize(fyne.NewSize(coverSize-4, coverSize-4))

	// right-aligned icon cluster, vertically centered
	center := func(o fyne.CanvasObject, right float32) float32 {
		ms := o.MinSize()
		o.Resize(ms)
		o.Move(fyne.NewPos(right-ms.Width, (size.Height-ms.Height)/2))
		return right - ms.Width
	}
	x := size.Width - pad
	x = center(m.loop, x) - 2
	x = center(m.next, x) - 1
	x = center(m.playpause, x) - 1
	x = center(m.prev, x) - 2
	x = center(m.shuffle, x) - 10
	x = center(m.addBtn, x) - 2
	x = center(m.favBtn, x)

	// title/artist stacked, vertically centered, truncating before the icons
	tx := pad + coverSize + 8
	titleH := m.title.MinSize().Height
	artistH := m.artist.MinSize().Height
	textH := titleH + artistH - 18
	tw := x - 6 - tx
	if tw < 0 {
		tw = 0
	}
	ty := (size.Height - textH) / 2
	m.title.Move(fyne.NewPos(tx-4, ty-5))
	m.title.Resize(fyne.NewSize(tw+4, titleH))
	m.artist.Move(fyne.NewPos(tx-4, ty+titleH-18))
	m.artist.Resize(fyne.NewSize(tw+4, artistH))
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

