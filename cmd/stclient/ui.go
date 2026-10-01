package main

import (
	"fmt"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

const (
	PrefJWT      = "jwt"
	PrefUsername = "username"
	PrefUserID   = "user_id"
	PrefCCURL    = "cc_url"
)

// AppContext holds shared client state and window navigation
type AppContext struct {
	App      fyne.App
	Window   fyne.Window
	CCClient *CCClient
	AuthFlow *DeviceAuthFlow
	JWT      string
	Username string
	UserID   string
}

// ShowLogin transitions the window content to the login screen
func (ctx *AppContext) ShowLogin() {
	ctx.Window.SetContent(ctx.makeLoginView())
}

// ShowDashboard transitions the window content to the authenticated dashboard
func (ctx *AppContext) ShowDashboard() {
	ctx.Window.SetContent(ctx.makeDashboardView())
}

// makeLoginView constructs the Twitch login and pairing interface
func (ctx *AppContext) makeLoginView() fyne.CanvasObject {
	title := widget.NewLabelWithStyle("STREAMTANKS", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	subtitle := widget.NewLabelWithStyle("Universal Artillery Client", fyne.TextAlignCenter, fyne.TextStyle{})

	statusLabel := widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{})
	codeLabel := widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	var dynamicArea *fyne.Container
	var loginBtn *widget.Button

	resetToInitial := func() {
		statusLabel.SetText("")
		codeLabel.SetText("")
		if loginBtn != nil {
			loginBtn.Enable()
		}
		if dynamicArea != nil {
			dynamicArea.Objects = []fyne.CanvasObject{}
			dynamicArea.Refresh()
		}
	}

	loginBtn = widget.NewButton("Log In with Twitch", func() {
		loginBtn.Disable()
		statusLabel.SetText("Requesting pairing code...")

		ctx.AuthFlow.Start(
			// On Code
			func(resp *DeviceCodeResponse) {
				fyne.Do(func() {
					codeLabel.SetText(resp.UserCode)
					statusLabel.SetText("Waiting for authorization in browser...")

					openBrowserBtn := widget.NewButton("Open Login in Browser", func() {
						if parsedURL, err := url.Parse(resp.VerificationURIComplete); err == nil {
							_ = ctx.App.OpenURL(parsedURL)
						}
					})

					cancelBtn := widget.NewButton("Cancel", func() {
						ctx.AuthFlow.Cancel()
						resetToInitial()
					})

					dynamicArea.Objects = []fyne.CanvasObject{
						widget.NewLabelWithStyle("Authorize pairing code:", fyne.TextAlignCenter, fyne.TextStyle{}),
						codeLabel,
						openBrowserBtn,
						cancelBtn,
					}
					dynamicArea.Refresh()

					// Auto-open browser initially
					if parsedURL, err := url.Parse(resp.VerificationURIComplete); err == nil {
						_ = ctx.App.OpenURL(parsedURL)
					}
				})
			},
			// On Approved
			func(resp *DevicePollResponse) {
				fyne.Do(func() {
					ctx.JWT = resp.Token
					ctx.Username = resp.Username
					ctx.UserID = resp.UserID

					// Persist credentials
					prefs := ctx.App.Preferences()
					prefs.SetString(PrefJWT, resp.Token)
					prefs.SetString(PrefUsername, resp.Username)
					prefs.SetString(PrefUserID, resp.UserID)

					ctx.ShowDashboard()
				})
			},
			// On Error
			func(err error) {
				fyne.Do(func() {
					statusLabel.SetText("Error: " + err.Error())
					resetToInitial()
				})
			},
		)
	})
	loginBtn.Importance = widget.HighImportance

	dynamicArea = container.NewVBox()

	form := container.NewVBox(
		layout.NewSpacer(),
		title,
		subtitle,
		widget.NewSeparator(),
		loginBtn,
		statusLabel,
		dynamicArea,
		layout.NewSpacer(),
	)

	return container.NewPadded(form)
}

// makeDashboardView constructs the authenticated player dashboard
func (ctx *AppContext) makeDashboardView() fyne.CanvasObject {
	title := widget.NewLabelWithStyle("STREAMTANKS", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	welcome := widget.NewLabelWithStyle(fmt.Sprintf("Welcome, %s!", ctx.Username), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	userInfo := widget.NewLabelWithStyle("Twitch User ID: "+ctx.UserID, fyne.TextAlignCenter, fyne.TextStyle{})

	hostsStatus := widget.NewLabel("Checking live channels...")

	// Fetch active hosts in background
	go func() {
		hosts, err := ctx.CCClient.GetActiveHosts()
		fyne.Do(func() {
			if err != nil {
				hostsStatus.SetText("C&C Relay: Unable to fetch live channels")
				return
			}
			if len(hosts) == 0 {
				hostsStatus.SetText("Live Channels: None currently active")
			} else {
				hostsStatus.SetText(fmt.Sprintf("Live Channels (%d): %s", len(hosts), strings.Join(hosts, ", ")))
			}
		})
	}()

	placeholderCard := widget.NewCard(
		"Channel Lobby & Game Controller",
		"Phase 3: Interactive Minimap & Protractor Aiming",
		widget.NewLabel("You are successfully authenticated!\nPhase 3 will render the interactive game controls here."),
	)

	logoutBtn := widget.NewButton("Log Out", func() {
		prefs := ctx.App.Preferences()
		prefs.RemoveValue(PrefJWT)
		prefs.RemoveValue(PrefUsername)
		prefs.RemoveValue(PrefUserID)

		ctx.JWT = ""
		ctx.Username = ""
		ctx.UserID = ""

		ctx.ShowLogin()
	})
	logoutBtn.Importance = widget.DangerImportance

	content := container.NewVBox(
		title,
		welcome,
		userInfo,
		widget.NewSeparator(),
		hostsStatus,
		placeholderCard,
		layout.NewSpacer(),
		logoutBtn,
	)

	return container.NewPadded(content)
}
