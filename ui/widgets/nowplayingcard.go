package widgets

import (
	"image"

	"fyne.io/fyne/v2/lang"

	"github.com/supersonic-app/supersonic/backend/mediaprovider"
	myTheme "github.com/supersonic-app/supersonic/ui/theme"
	"github.com/supersonic-app/supersonic/ui/util"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Shows the current album art, track name, and artist name
// for the currently playing track. Placed into the left side of the BottomPanel.
type NowPlayingCard struct {
	widget.BaseWidget

	DisableRating bool

	trackName   *OptionHyperlink
	artistName  *MultiHyperlink
	cover       *ImagePlaceholder
	addPlaylist *IconButton
	menuBtn     *IconButton
	menu        *widget.PopUpMenu
	ratingMenu  *fyne.MenuItem
	textLayout  textWithTrailingButtonsLayout

	OnTrackNameTapped  func()
	OnArtistNameTapped func(artistID string)
	OnCoverTapped      func()
	OnSetRating        func(rating int)
	OnSetFavorite      func(favorite bool)
	OnAddToPlaylist    func()
	OnShowTrackInfo    func()
	OnShare            func()
}

func NewNowPlayingCard() *NowPlayingCard {
	n := &NowPlayingCard{
		trackName:  NewOptionHyperlink(),
		artistName: NewMultiHyperlink(),
	}
	n.ExtendBaseWidget(n)
	n.cover = NewImagePlaceholder(myTheme.TracksIcon, 62)
	n.cover.OnTapped = n.onShowCoverImage
	n.cover.ScaleMode = canvas.ImageScaleFastest
	n.cover.Hidden = true
	n.trackName.Hidden = true
	// smaller text throughout; the bold track name keeps the hierarchy
	n.trackName.SetTextStyle(fyne.TextStyle{Bold: true})
	n.trackName.SetSizeName(myTheme.SizeNameSubText)
	n.artistName.SizeName = myTheme.SizeNameSubText
	// the options menu lives on a standalone button next to the
	// quick add-to-playlist button, not attached to the track name
	n.trackName.SetMenuBtnEnabled(false)
	n.artistName.OnTapped = n.onArtistNameTapped
	n.trackName.SetOnTapped(n.onTrackNameTapped)

	n.addPlaylist = NewIconButton(theme.ContentAddIcon(), n.onAddToPlaylist)
	n.addPlaylist.IconSize = IconButtonSizeSmaller
	n.addPlaylist.SetToolTip(lang.L("Add to playlist"))
	n.addPlaylist.Disable()
	n.menuBtn = NewIconButton(theme.MoreVerticalIcon(), func() {
		n.showMenu(fyne.CurrentApp().Driver().AbsolutePositionForObject(n.menuBtn))
	})
	n.menuBtn.IconSize = IconButtonSizeSmaller
	n.menuBtn.SetToolTip(lang.L("More options"))
	n.menuBtn.Disable()

	return n
}

func (n *NowPlayingCard) MinSize() fyne.Size {
	// prop up height for when cover image is hidden
	return fyne.NewSize(n.BaseWidget.MinSize().Width, 72)
}

func (n *NowPlayingCard) onArtistNameTapped(artistID string) {
	if n.OnArtistNameTapped != nil {
		n.OnArtistNameTapped(artistID)
	}
}

func (n *NowPlayingCard) onTrackNameTapped() {
	if n.OnTrackNameTapped != nil {
		n.OnTrackNameTapped()
	}
}

func (n *NowPlayingCard) onShowCoverImage(*fyne.PointEvent) {
	if n.OnCoverTapped != nil {
		n.OnCoverTapped()
	}
}

func (n *NowPlayingCard) onSetFavorite(fav bool) {
	if n.OnSetFavorite != nil {
		n.OnSetFavorite(fav)
	}
}

func (n *NowPlayingCard) onSetRating(rating int) {
	if n.OnSetRating != nil {
		n.OnSetRating(rating)
	}
}

func (n *NowPlayingCard) onAddToPlaylist() {
	if n.OnAddToPlaylist != nil {
		n.OnAddToPlaylist()
	}
}

func (n *NowPlayingCard) onShowTrackInfo() {
	if n.OnShowTrackInfo != nil {
		n.OnShowTrackInfo()
	}
}

func (n *NowPlayingCard) onShare() {
	if n.OnShare != nil {
		n.OnShare()
	}
}

func (n *NowPlayingCard) CreateRenderer() fyne.WidgetRenderer {
	// pad the cover so it doesn't touch the window corner/edges
	paddedCover := container.New(
		&layout.CustomPaddedLayout{LeftPadding: 5, TopPadding: 4, BottomPadding: 5},
		n.cover)
	// quick add-to-playlist and more options, vertically centered,
	// with a gap between the two buttons
	actionBtns := container.NewVBox(layout.NewSpacer(),
		container.NewHBox(n.addPlaylist, util.NewHSpace(6), n.menuBtn),
		layout.NewSpacer())
	// text rows vertically centered so they share a midline with the buttons
	textBlock := container.NewVBox(layout.NewSpacer(),
		container.New(layout.NewCustomPaddedVBoxLayout(theme.Padding()-13), n.trackName, n.artistName),
		layout.NewSpacer())
	// custom layout keeps the action buttons right after the (possibly
	// truncated) text block, with padding so they never touch it
	n.textLayout.gap = 10
	c := container.NewBorder(nil, nil, paddedCover, nil,
		container.New(&n.textLayout, textBlock, actionBtns))
	return widget.NewSimpleRenderer(c)
}

// textWithTrailingButtonsLayout lays out two objects: a text block that
// may be truncated, and a buttons block placed immediately after the
// text's natural width (plus a gap), never overlapping it.
// textWidth must be set to the text's natural width by the owner;
// the truncating text widgets report useless MinSize widths.
type textWithTrailingButtonsLayout struct {
	gap       float32
	textWidth float32
}

func (l *textWithTrailingButtonsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	btns := objects[1].MinSize()
	return fyne.NewSize(50+l.gap+btns.Width, fyne.Max(objects[0].MinSize().Height, btns.Height))
}

func (l *textWithTrailingButtonsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	btnW := objects[1].MinSize().Width
	textW := fyne.Max(0, fyne.Min(l.textWidth, size.Width-btnW-l.gap))
	objects[0].Resize(fyne.NewSize(textW, size.Height))
	objects[0].Move(fyne.NewPos(0, 0))
	objects[1].Resize(fyne.NewSize(btnW, size.Height))
	objects[1].Move(fyne.NewPos(textW+l.gap, 0))
}

func (n *NowPlayingCard) Update(track mediaprovider.MediaItem) {
	if track == nil {
		n.trackName.SetTextAndToolTip("")
		n.artistName.BuildSegments([]string{}, []string{})
		n.cover.Hidden = true
	} else {
		n.cover.Hidden = false
		n.trackName.SetTextAndToolTip(track.Metadata().Name)
		if tr, ok := track.(*mediaprovider.Track); ok {
			n.artistName.BuildSegments(tr.ArtistNames, tr.ArtistIDs)
			n.cover.PlaceholderIcon = myTheme.TracksIcon
		} else {
			n.artistName.BuildSegments([]string{}, []string{})
			n.cover.PlaceholderIcon = myTheme.RadioIcon
		}
	}
	n.trackName.Hidden = n.trackName.Text() == ""
	// playlist add / options only apply to real tracks (not radio, not stopped)
	if _, isTrack := track.(*mediaprovider.Track); isTrack && n.cover.PlaceholderIcon != myTheme.RadioIcon {
		n.addPlaylist.Enable()
		n.menuBtn.Enable()
	} else {
		n.addPlaylist.Disable()
		n.menuBtn.Disable()
	}
	n.artistName.Hidden = len(n.artistName.Segments) == 0
	// natural text width drives where the action buttons sit
	n.textLayout.textWidth = fyne.Max(n.trackName.PreferredWidth(), n.artistName.PreferredWidth())
	n.Refresh()
}

func (n *NowPlayingCard) SetImage(cover image.Image) {
	n.cover.SetImage(cover, true)
}

func (n *NowPlayingCard) showMenu(btnPos fyne.Position) {
	if n.menu == nil {
		n.ratingMenu = util.NewRatingSubmenu(n.onSetRating)
		favorite := fyne.NewMenuItem(lang.L("Set favorite"), func() { n.onSetFavorite(true) })
		favorite.Icon = myTheme.FavoriteIcon
		unfavorite := fyne.NewMenuItem(lang.L("Unset favorite"), func() { n.onSetFavorite(false) })
		unfavorite.Icon = myTheme.NotFavoriteIcon
		playlist := fyne.NewMenuItem(lang.L("Add to playlist")+"...", func() { n.onAddToPlaylist() })
		playlist.Icon = myTheme.PlaylistIcon
		info := fyne.NewMenuItem(lang.L("Show info")+"...", func() { n.onShowTrackInfo() })
		info.Icon = theme.InfoIcon()
		share := fyne.NewMenuItem(lang.L("Share")+"...", func() { n.onShare() })
		share.Icon = myTheme.ShareIcon

		m := fyne.NewMenu("", favorite, unfavorite, n.ratingMenu, playlist, info, share)
		n.menu = widget.NewPopUpMenu(m, fyne.CurrentApp().Driver().CanvasForObject(n))
	}
	menuSize := n.menu.MinSize()
	n.ratingMenu.Disabled = n.DisableRating
	btnPos.Y -= (menuSize.Height + theme.Padding()*3)
	btnPos.X -= menuSize.Width / 2
	n.menu.ShowAtPosition(btnPos)
}
