package main

import (
	"errors"
	"net/http"
)

// HostAuthenticator defines how a streamer's local instance proves they own a channel.
// Using an interface allows us to easily swap out the authentication strategy
// (e.g., from TrustFirst to SharedSecret to TwitchOAuth) without rewriting the Hub.
type HostAuthenticator interface {
	// Authenticate inspects the incoming HTTP upgrade request and returns
	// the authenticated channel ID, or an error if authentication fails.
	Authenticate(r *http.Request) (string, error)
}

// TrustFirstAuthenticator implements HostAuthenticator by blindly trusting
// the channel name provided in the query string (e.g., ?channel=mrpoundsign).
// It relies on the Hub to reject the connection if the channel is already claimed.
type TrustFirstAuthenticator struct{}

func (a *TrustFirstAuthenticator) Authenticate(r *http.Request) (string, error) {
	channel := r.URL.Query().Get("channel")
	if channel == "" {
		return "", errors.New("missing channel parameter")
	}
	return channel, nil
}
