//go:build demo

// Command mockplayer serves a fictional MPRIS media player on the D-Bus
// session bus under the Spotify desktop bus name, so the real spmeta binary
// can read it while recording the README demo. It is built only with the
// "demo" tag, which keeps it out of go build ./..., the release binaries, and
// the linters.
//
// Control it by writing a command to the trigger file: "next", "pause", or
// "play". The player reads the file, deletes it, and applies the command,
// emitting PropertiesChanged as a real player does.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

const (
	busName     = "org.mpris.MediaPlayer2.spotify"
	playerPath  = "/org/mpris/MediaPlayer2"
	playerIface = "org.mpris.MediaPlayer2.Player"
	rootIface   = "org.mpris.MediaPlayer2"

	tickInterval    = 500 * time.Millisecond
	triggerInterval = 100 * time.Millisecond
	startPosition   = 72 * time.Second
)

// track is a fictional item. The track id is an object path that spmeta does
// not map to a spotify:track: URI, so the demo output carries no link that
// looks real.
type track struct {
	id     string
	title  string
	artist string
	album  string
	number int32
	length time.Duration
}

var playlist = []track{
	{"/org/example/demo/signal_path", "Signal Path", "The Metadata Ensemble", "Field Notes", 1, 3*time.Minute + 41*time.Second},
	{"/org/example/demo/quiet_pipeline", "Quiet Pipeline", "Null Device Quartet", "Exit Codes", 3, 4*time.Minute + 6*time.Second},
	{"/org/example/demo/last_write_wins", "Last Write Wins", "The Metadata Ensemble", "Field Notes", 7, 3*time.Minute + 12*time.Second},
}

type player struct {
	props    *prop.Properties
	current  int
	position time.Duration
	playing  bool
}

func main() {
	trigger := os.Getenv("DEMO_TRIGGER")
	if trigger == "" {
		trigger = "/tmp/spmeta-demo-trigger"
	}
	if err := run(trigger); err != nil {
		fmt.Fprintln(os.Stderr, "mockplayer:", err)
		os.Exit(1)
	}
}

func run(trigger string) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("connect to the session bus: %w", err)
	}
	defer func() { _ = conn.Close() }()

	p := &player{position: startPosition, playing: true}
	p.props, err = prop.Export(conn, playerPath, prop.Map{
		playerIface: {
			"PlaybackStatus": {Value: "Playing", Emit: prop.EmitTrue},
			"Metadata":       {Value: metadata(playlist[0]), Emit: prop.EmitTrue},
			"Position":       {Value: p.positionMicros()},
		},
		rootIface: {"Identity": {Value: "Demo Player"}},
	})
	if err != nil {
		return fmt.Errorf("export properties: %w", err)
	}
	reply, err := conn.RequestName(busName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return fmt.Errorf("request %s: %w", busName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("%s is already owned", busName)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	tick := time.NewTicker(tickInterval)
	poll := time.NewTicker(triggerInterval)
	defer tick.Stop()
	defer poll.Stop()
	for {
		select {
		case <-stop:
			return nil
		case <-tick.C:
			p.advance(tickInterval)
		case <-poll.C:
			p.apply(readCommand(trigger))
		}
	}
}

// readCommand returns and removes the command in the trigger file, or "".
func readCommand(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	_ = os.Remove(path)
	return strings.TrimSpace(string(data))
}

func (p *player) apply(command string) {
	switch command {
	case "next":
		p.switchTo((p.current + 1) % len(playlist))
	case "pause":
		p.playing = false
		p.props.SetMust(playerIface, "PlaybackStatus", "Paused")
	case "play":
		p.playing = true
		p.props.SetMust(playerIface, "PlaybackStatus", "Playing")
	}
}

// advance moves the position while playing. Position changes are not
// signalled, as in the MPRIS specification. A finished track starts the next.
func (p *player) advance(d time.Duration) {
	if !p.playing {
		return
	}
	p.position += d
	if p.position >= playlist[p.current].length {
		p.switchTo((p.current + 1) % len(playlist))
		return
	}
	p.props.SetMust(playerIface, "Position", p.positionMicros())
}

func (p *player) switchTo(index int) {
	p.current = index
	p.position = 0
	p.props.SetMust(playerIface, "Position", p.positionMicros())
	p.props.SetMust(playerIface, "Metadata", metadata(playlist[index]))
}

func (p *player) positionMicros() int64 {
	return p.position.Microseconds()
}

func metadata(t track) map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"mpris:trackid":     dbus.MakeVariant(dbus.ObjectPath(t.id)),
		"mpris:length":      dbus.MakeVariant(t.length.Microseconds()),
		"xesam:title":       dbus.MakeVariant(t.title),
		"xesam:artist":      dbus.MakeVariant([]string{t.artist}),
		"xesam:albumArtist": dbus.MakeVariant([]string{t.artist}),
		"xesam:album":       dbus.MakeVariant(t.album),
		"xesam:trackNumber": dbus.MakeVariant(t.number),
	}
}
