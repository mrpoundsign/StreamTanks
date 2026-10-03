package assets

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed icon.png
var IconPNG []byte

//go:embed icon.svg
var IconSVG []byte

// AppIcon returns the static Fyne resource for the StreamTanks Game Client icon.
func AppIcon() fyne.Resource {
	return fyne.NewStaticResource("icon.png", IconPNG)
}
