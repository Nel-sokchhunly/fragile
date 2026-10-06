package main

import (
	"context"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Event names. Keep in sync with frontend/src/lib/events.ts.
const eventTick = "app:tick"

// tickEvent is the payload of eventTick.
type tickEvent struct {
	Count int    `json:"count"`
	At    string `json:"at"` // RFC 3339
}

// App is the Wails application. Backend state changes reach the UI only as
// events (Go -> Wails events -> Zustand store -> components); nothing polls.
type App struct{}

func NewApp() *App { return &App{} }

// startup runs once when the window is created; ctx lives until the app quits.
func (a *App) startup(ctx context.Context) {
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for count := 1; ; count++ {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				runtime.EventsEmit(ctx, eventTick, tickEvent{Count: count, At: now.Format(time.RFC3339)})
			}
		}
	}()
}
