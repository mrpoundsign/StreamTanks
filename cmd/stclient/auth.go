package main

import (
	"context"
	"errors"
	"sync"
	"time"
)

// DeviceAuthFlow coordinates requesting a device code and polling until approved or cancelled
type DeviceAuthFlow struct {
	client   *CCClient
	mu       sync.Mutex
	cancelFn context.CancelFunc
}

func NewDeviceAuthFlow(client *CCClient) *DeviceAuthFlow {
	return &DeviceAuthFlow{
		client: client,
	}
}

// Start begins the device code request and starts polling in a background goroutine
func (f *DeviceAuthFlow) Start(
	onCode func(resp *DeviceCodeResponse),
	onApproved func(resp *DevicePollResponse),
	onError func(err error),
) {
	f.mu.Lock()
	if f.cancelFn != nil {
		f.cancelFn()
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.cancelFn = cancel
	f.mu.Unlock()

	go func() {
		// 1. Request device code
		codeResp, err := f.client.RequestDeviceCode()
		if err != nil {
			onError(err)
			return
		}

		if onCode != nil {
			onCode(codeResp)
		}

		interval := time.Duration(codeResp.Interval) * time.Second
		if interval < time.Second {
			interval = 2 * time.Second
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		timeout := time.After(time.Duration(codeResp.ExpiresIn) * time.Second)

		for {
			select {
			case <-ctx.Done():
				return
			case <-timeout:
				onError(errors.New("pairing session expired"))
				return
			case <-ticker.C:
				pollResp, err := f.client.PollDeviceAuth(codeResp.DeviceCode)
				if err != nil {
					// Network error during poll, retry on next tick
					continue
				}

				if pollResp.Status == "approved" {
					if onApproved != nil {
						onApproved(pollResp)
					}
					return
				}
			}
		}
	}()
}

// Cancel stops an in-flight polling loop
func (f *DeviceAuthFlow) Cancel() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cancelFn != nil {
		f.cancelFn()
		f.cancelFn = nil
	}
}
