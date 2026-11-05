package ui

import "github.com/gen2brain/beeep"

// Notify shows a desktop notification. The platform's notification service
// owns the toast, so the call returns without waiting for it to disappear.
//
// On Linux it goes through D-Bus (falling back to notify-send), on macOS
// through the notification center and on Windows through the toast API.
func Notify(title, body string) error {
	return beeep.Notify(title, body, "")
}
