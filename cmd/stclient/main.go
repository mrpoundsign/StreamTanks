package main

import (
	"flag"
	"strings"

	"streamtanks/assets"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
)

func main() {
	ccURLFlag := flag.String("cc", "", "StreamTanks C&C Relay Server URL")
	flag.Parse()

	myApp := app.NewWithID("com.poundsigndesign.streamtanks.client")
	icon := assets.AppIcon()
	myApp.SetIcon(icon)
	myApp.Settings().SetTheme(&CyberpunkTheme{})

	prefs := myApp.Preferences()
	ccURL := strings.TrimSpace(*ccURLFlag)
	if ccURL == "" {
		ccURL = prefs.StringWithFallback(PrefCCURL, "https://st-cc.poundsigndesign.com")
	} else {
		prefs.SetString(PrefCCURL, ccURL)
	}

	ccClient := NewCCClient(ccURL)
	authFlow := NewDeviceAuthFlow(ccClient)

	win := myApp.NewWindow("StreamTanks Game Client")
	win.SetIcon(icon)
	win.Resize(fyne.NewSize(420, 680))

	ctx := &AppContext{
		App:      myApp,
		Window:   win,
		CCClient: ccClient,
		AuthFlow: authFlow,
		JWT:      prefs.String(PrefJWT),
		Username: prefs.String(PrefUsername),
		UserID:   prefs.String(PrefUserID),
	}

	if ctx.JWT != "" && ctx.Username != "" {
		ctx.ShowDashboard()
	} else {
		ctx.ShowLogin()
	}

	win.ShowAndRun()
}
