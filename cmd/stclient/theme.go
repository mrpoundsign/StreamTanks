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
		return color.NRGBA{R: 0x1a, G: 0x24, B: 0x30, A: 0xff} // Dark slate
	case theme.ColorNamePrimary:
		return color.NRGBA{R: 0x00, G: 0xe6, B: 0xb8, A: 0xff} // High-saturation Neon Cyan
	case theme.ColorNameForeground:
		return color.NRGBA{R: 0xe6, G: 0xf1, B: 0xff, A: 0xff} // Bright high-contrast text on dark surfaces
	case theme.ColorNameForegroundOnPrimary, theme.ColorNameForegroundOnSuccess, theme.ColorNameForegroundOnWarning:
		return color.NRGBA{R: 0x05, G: 0x0d, B: 0x1a, A: 0xff} // Deep dark navy text on bright cyan/green buttons (11:1 contrast)
	case theme.ColorNameForegroundOnError:
		return color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff} // White text on red buttons
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 0x13, G: 0x18, B: 0x22, A: 0xff}
	case theme.ColorNameError:
		return color.NRGBA{R: 0xff, G: 0x3b, B: 0x4e, A: 0xff} // High-contrast Red
	case theme.ColorNameSuccess:
		return color.NRGBA{R: 0x00, G: 0xcc, B: 0x66, A: 0xff} // High-contrast Green
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
