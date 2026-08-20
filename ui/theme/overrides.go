package theme

import (
	"image/color"

	"fyne.io/fyne/v2"
)

type colorOverrideTheme struct {
	colorTransformName fyne.ThemeColorName
	colorTransform     func(color.Color) color.Color
}

func WithColorTransformOverride(name fyne.ThemeColorName, transform func(color.Color) color.Color) fyne.Theme {
	return &colorOverrideTheme{
		colorTransformName: name,
		colorTransform:     transform,
	}
}

var _ fyne.Theme = (*colorOverrideTheme)(nil)

func (c *colorOverrideTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	col := fyne.CurrentApp().Settings().Theme().Color(n, v)
	if n == c.colorTransformName {
		return c.colorTransform(col)
	}
	return col
}

func (*colorOverrideTheme) Font(s fyne.TextStyle) fyne.Resource {
	return fyne.CurrentApp().Settings().Theme().Font(s)
}

func (*colorOverrideTheme) Icon(s fyne.ThemeIconName) fyne.Resource {
	return fyne.CurrentApp().Settings().Theme().Icon(s)
}

func (*colorOverrideTheme) Size(s fyne.ThemeSizeName) float32 {
	return fyne.CurrentApp().Settings().Theme().Size(s)
}

type sizeOverrideTheme struct {
	sizeName fyne.ThemeSizeName
	value    float32
}

// WithSizeOverride returns a theme that reports the given value for the
// given size name, delegating everything else to the app theme.
func WithSizeOverride(name fyne.ThemeSizeName, value float32) fyne.Theme {
	return &sizeOverrideTheme{sizeName: name, value: value}
}

var _ fyne.Theme = (*sizeOverrideTheme)(nil)

func (s *sizeOverrideTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	return fyne.CurrentApp().Settings().Theme().Color(n, v)
}

func (*sizeOverrideTheme) Font(st fyne.TextStyle) fyne.Resource {
	return fyne.CurrentApp().Settings().Theme().Font(st)
}

func (*sizeOverrideTheme) Icon(i fyne.ThemeIconName) fyne.Resource {
	return fyne.CurrentApp().Settings().Theme().Icon(i)
}

func (s *sizeOverrideTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == s.sizeName {
		return s.value
	}
	return fyne.CurrentApp().Settings().Theme().Size(n)
}
