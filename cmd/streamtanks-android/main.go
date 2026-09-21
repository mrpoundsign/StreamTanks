package main

import (
	"fmt"
	"log"
	"net/url"

	"fyne.io/fyne/v2"
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

// URLManager defines the interface for interacting with application URLs.
type URLManager interface {
	CopyOverlayURL()
	CopyAdminURL()
	CopyPreviewURL()
	OpenAdminURL()
	OpenPreviewURL()
}

type appURLManager struct {
	clipboard  fyne.Clipboard
	app        fyne.App
	baseURL    string
	adminURL   string
	previewURL string
}

func newAppURLManager(clipboard fyne.Clipboard, app fyne.App, baseURL string) URLManager {
	return &appURLManager{
		clipboard:  clipboard,
		app:        app,
		baseURL:    baseURL,
		adminURL:   fmt.Sprintf("%s/admin", baseURL),
		previewURL: fmt.Sprintf("%s/preview.html", baseURL),
	}
}

func (m *appURLManager) CopyOverlayURL() {
	m.clipboard.SetContent(m.baseURL)
}

func (m *appURLManager) CopyAdminURL() {
	m.clipboard.SetContent(m.adminURL)
}

func (m *appURLManager) CopyPreviewURL() {
	m.clipboard.SetContent(m.previewURL)
}

func (m *appURLManager) OpenAdminURL() {
	if u, err := url.Parse(m.adminURL); err == nil {
		m.app.OpenURL(u)
	}
}

func (m *appURLManager) OpenPreviewURL() {
	if u, err := url.Parse(m.previewURL); err == nil {
		m.app.OpenURL(u)
	}
}

func main() {
	a := app.New()
	w := a.NewWindow("StreamTanks")

	addr := "127.0.0.1:8102"
	baseURL := fmt.Sprintf("http://%s", addr)

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

	manager := newAppURLManager(w.Clipboard(), a, baseURL)

	content := container.NewVBox(
		widget.NewLabel("StreamTanks Server is running on:"),
		widget.NewLabel(baseURL),
		widget.NewButton("Copy Overlay URL", manager.CopyOverlayURL),
		widget.NewButton("Copy Admin URL", manager.CopyAdminURL),
		widget.NewButton("Copy Preview URL", manager.CopyPreviewURL),
		widget.NewButton("Open Admin", manager.OpenAdminURL),
		widget.NewButton("Open Preview", manager.OpenPreviewURL),
	)

	w.SetContent(content)
	w.ShowAndRun()
}
