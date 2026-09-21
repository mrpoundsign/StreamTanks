#!/bin/bash
set -e

# Make sure you have the Fyne CLI installed, and android NDK setup.
# go install fyne.io/fyne/v2/cmd/fyne@latest

echo "Building StreamTanks Android App..."
fyne package -os android -appID com.mrpoundsign.streamtanks -name StreamTanks -icon Icon.png -src ./cmd/streamtanks-android
