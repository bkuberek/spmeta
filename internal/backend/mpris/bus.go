// Package mpris reads Spotify clients on Linux through the MPRIS D-Bus
// interface. Selection and mapping are plain Go; the godbus adapter that
// implements Bus lives in dbus_linux.go. See docs/design/backend-linux.md.
package mpris

import "context"

// busPrefix starts the bus name of every MPRIS player.
const busPrefix = "org.mpris.MediaPlayer2."

// Properties is the reply of Properties.GetAll on
// org.mpris.MediaPlayer2.Player, with D-Bus variants unwrapped to plain Go
// values. The keys the backend reads are:
//
//	PlaybackStatus  string          "Playing", "Paused" or "Stopped"
//	Position        int64           microseconds
//	Metadata        map[string]any  same value rules, keyed "xesam:title", ...
//
// Within Metadata, object paths (mpris:trackid) arrive as string, mpris:length
// as int64 microseconds, and xesam:artist as []string. The mapping also
// accepts the off-spec shapes players are known to send: any integer width or
// float64 for numbers, and a single string or []any for string lists.
type Properties map[string]any

// Bus is the seam between the backend and the session bus. The godbus adapter
// implements it; tests use a fake.
type Bus interface {
	// ListNames returns every name on the bus, MPRIS or not.
	ListNames(ctx context.Context) ([]string, error)
	// GetAll returns the Player interface properties of busName, a full name
	// such as "org.mpris.MediaPlayer2.spotify".
	GetAll(ctx context.Context, busName string) (Properties, error)
	// Close releases the connection.
	Close() error
}

// signaler is the optional part of a Bus that reports player changes. The
// godbus adapter implements it.
type signaler interface {
	// Signals subscribes to the signals that announce a player change and
	// returns a channel that receives a value for each, at most one pending.
	// The channel closes when the connection ends. ctx bounds the
	// subscription calls only.
	Signals(ctx context.Context) (<-chan struct{}, error)
}

// DialFunc connects to the session bus. It may return a *backend.Error, which
// Read passes through unchanged; any other error becomes "not available".
// ctx stays live for the whole read, so an implementation may tie the
// connection's lifetime to it.
type DialFunc func(ctx context.Context) (Bus, error)
