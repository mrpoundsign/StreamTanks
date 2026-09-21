#!/bin/bash
set -e

# Make sure you have the Fyne CLI installed, and android NDK setup.
# go install fyne.io/fyne/v2/cmd/fyne@latest

export CGO_ENABLED=1
echo "Building StreamTanks Android App..."
go run fyne.io/fyne/v2/cmd/fyne@latest package -os android -appID com.mrpoundsign.streamtanks -name StreamTanks -icon Icon.png -src ./cmd/streamtanks-android
