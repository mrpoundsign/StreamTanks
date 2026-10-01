package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// CyberpunkTheme provides the custom neon cyberpunk styling for StreamTanks
type CyberpunkTheme struct{}

var _ fyne.Theme = (*CyberpunkTheme)(nil)

func (t *CyberpunkTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.NRGBA{R: 0x0b, G: 0x0c, B: 0x10, A: 0xff} // #0b0c10
	case theme.ColorNameButton:
		return color.NRGBA{R: 0x1f, G: 0x28, B: 0x33, A: 0xff} // #1f2833
	case theme.ColorNamePrimary:
		return color.NRGBA{R: 0x66, G: 0xfc, B: 0xf1, A: 0xff} // Neon Cyan #66fcf1
	case theme.ColorNameForeground:
		return color.NRGBA{R: 0xc5, G: 0xc6, B: 0xc7, A: 0xff} // Light Gray
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 0x15, G: 0x19, B: 0x22, A: 0xff}
	default:
		return theme.DefaultTheme().Color(name, theme.VariantDark)
	}
}

func (t *CyberpunkTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (t *CyberpunkTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (t *CyberpunkTheme) Size(name fyne.ThemeSizeName) float32 {
	return theme.DefaultTheme().Size(name)
}
