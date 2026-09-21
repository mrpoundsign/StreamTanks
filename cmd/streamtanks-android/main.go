package main

import (
	"fmt"
	"log"
	"net/url"
	"time"

	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	streamapp "streamtanks/internal/app"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	a := app.New()
	w := a.NewWindow("StreamTanks")

	addr := "127.0.0.1:8102"

	// Start server in background
	go func() {
		cfg := streamapp.Config{
			ListenAddr:  addr,
			DebugMode:   false,
			BouncyWalls: false,
			CCServerURL: "",
			Version:     version,
			Commit:      commit,
			Date:        date,
		}

		if err := streamapp.Run(cfg); err != nil {
			log.Println("Server stopped:", err)
		}
	}()

	// Wait for server to start
	time.Sleep(1 * time.Second)

	baseURL := fmt.Sprintf("http://%s", addr)
	adminURL := fmt.Sprintf("%s/admin", baseURL)
	previewURL := fmt.Sprintf("%s/preview.html", baseURL)

	copyOverlayBtn := widget.NewButton("Copy Overlay URL", func() {
		w.Clipboard().SetContent(baseURL)
	})

	openAdminBtn := widget.NewButton("Open Admin", func() {
		u, _ := url.Parse(adminURL)
		a.OpenURL(u)
	})

	openPreviewBtn := widget.NewButton("Open Preview", func() {
		u, _ := url.Parse(previewURL)
		a.OpenURL(u)
	})

	content := container.NewVBox(
		widget.NewLabel("StreamTanks Server is running on:"),
		widget.NewLabel(baseURL),
		copyOverlayBtn,
		openAdminBtn,
		openPreviewBtn,
	)

	w.SetContent(content)
	w.ShowAndRun()
}
