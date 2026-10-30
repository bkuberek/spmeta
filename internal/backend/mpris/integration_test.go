//go:build linux && integration

package mpris

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/format"
)

// Run under a private bus: dbus-run-session -- go test -tags integration ./internal/backend/mpris/...

type served struct {
	suffix  string
	fixture string // name in goldenFixtures
}

func TestIntegrationRead(t *testing.T) {
	tests := []struct {
		name   string
		serve  []served
		player string
		golden string
	}{
		{
			name:   "spotify desktop",
			serve:  []served{{"spotify", "spotify_desktop_playing"}},
			golden: "spotify_desktop_playing",
		},
		{
			name:   "spotifyd instance",
			serve:  []served{{"spotifyd.instance42", "spotifyd_playing"}},
			golden: "spotifyd_playing",
		},
		{
			name:   "paused desktop still wins over playing spotifyd",
			serve:  []served{{"spotify", "paused"}, {"spotifyd.instance42", "spotifyd_playing"}},
			golden: "paused",
		},
		{
			name:   "player flag picks the suffix",
			serve:  []served{{"spotify", "paused"}, {"spotifyd.instance42", "spotifyd_playing"}},
			player: "spotifyd",
			golden: "spotifyd_playing",
		},
		{
			name:   "spotify_player without trackid",
			serve:  []served{{"spotify_player", "spotify_player_no_trackid"}},
			golden: "spotify_player_no_trackid",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := busAddress(t)
			for _, s := range tt.serve {
				servePlayer(t, addr, s.suffix, fixtureProps(t, s.fixture))
			}
			b := New(Dial, tt.player, func() time.Time { return fetchedAt })
			np, err := b.Read(context.Background())
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			got, err := format.JSON(&np)
			if err != nil {
				t.Fatal(err)
			}
			checkGolden(t, tt.golden, got)
		})
	}
}

func TestIntegrationPlayers(t *testing.T) {
	addr := busAddress(t)
	servePlayer(t, addr, "spotify", fixtureProps(t, "paused"))
	servePlayer(t, addr, "spotifyd.instance42", fixtureProps(t, "spotifyd_playing"))

	b := New(Dial, "", time.Now)
	got, err := b.Players(context.Background())
	if err != nil {
		t.Fatalf("Players: %v", err)
	}
	want := []PlayerInfo{
		{Name: "spotify", Status: "Paused", Selected: true},
		{Name: "spotifyd.instance42", Status: "Playing"},
	}
	if len(got) != len(want) {
		t.Fatalf("Players = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Players[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestIntegrationNoPlayer(t *testing.T) {
	busAddress(t)
	_, err := New(Dial, "", time.Now).Read(context.Background())
	requireNotAvailable(t, err, "no Spotify MPRIS player found")
}

func TestIntegrationUnreachableBus(t *testing.T) {
	t.Setenv(envSessionBus, "unix:path=/nonexistent/spmeta-test-bus")
	_, err := New(Dial, "", time.Now).Read(context.Background())
	requireNotAvailable(t, err, "could not reach the D-Bus session bus")
}

func TestIntegrationNoBusAddress(t *testing.T) {
	t.Setenv(envSessionBus, "")
	t.Setenv(envRuntimeDir, t.TempDir())
	_, err := New(Dial, "", time.Now).Read(context.Background())
	requireNotAvailable(t, err, "no D-Bus session bus found")
}

func requireNotAvailable(t *testing.T, err error, message string) {
	t.Helper()
	var be *backend.Error
	if !errors.As(err, &be) {
		t.Fatalf("error = %v, want *backend.Error", err)
	}
	if be.Kind != backend.KindNotAvailable || be.Message != message {
		t.Errorf("error = {%v %q}, want {not available %q}", be.Kind, be.Message, message)
	}
}

func busAddress(t *testing.T) string {
	t.Helper()
	addr, err := sessionBusAddress(os.Getenv, isSocket)
	if err != nil {
		t.Fatalf("no session bus; run under dbus-run-session: %v", err)
	}
	return addr
}

func fixtureProps(t *testing.T, name string) Properties {
	t.Helper()
	for _, f := range goldenFixtures() {
		if f.name == name {
			return f.props
		}
	}
	t.Fatalf("unknown fixture %q", name)
	return nil
}

// rootIface is the MPRIS root interface, whose changes must not wake.
const rootIface = "org.mpris.MediaPlayer2"

// mockPlayer is a player served on the test bus.
type mockPlayer struct {
	conn  *dbus.Conn
	props *prop.Properties
}

// servePlayer exports a mock MPRIS player on its own connection, with real
// D-Bus types: an object path for local-style track ids, plain strings
// otherwise. Setting PlaybackStatus, Metadata, or the root interface's
// Identity emits PropertiesChanged, as real players do.
func servePlayer(t *testing.T, addr, suffix string, p Properties) mockPlayer {
	t.Helper()
	conn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatalf("connect mock player: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	meta := make(map[string]dbus.Variant)
	raw, _ := p["Metadata"].(map[string]any)
	for k, v := range raw {
		if s, ok := v.(string); ok && k == "mpris:trackid" && strings.HasPrefix(s, "/") {
			v = dbus.ObjectPath(s)
		}
		meta[k] = dbus.MakeVariant(v)
	}
	player := map[string]*prop.Prop{
		"PlaybackStatus": {Value: p["PlaybackStatus"], Emit: prop.EmitTrue},
		"Position":       {Value: p["Position"]},
		"Metadata":       {Value: meta, Emit: prop.EmitTrue},
	}
	root := map[string]*prop.Prop{"Identity": {Value: suffix, Emit: prop.EmitTrue}}
	props, err := prop.Export(conn, playerPath, prop.Map{playerIface: player, rootIface: root})
	if err != nil {
		t.Fatalf("export mock player: %v", err)
	}
	requestName(t, conn, busPrefix+suffix)
	return mockPlayer{conn: conn, props: props}
}

func TestIntegrationChanges(t *testing.T) {
	addr := busAddress(t)

	t.Run("a Player property change wakes", func(t *testing.T) {
		player := servePlayer(t, addr, "spotify", fixtureProps(t, "paused"))
		waitForPlayers(t, addr, "spotify")
		wake := listen(t)
		player.props.SetMust(rootIface, "Identity", "Spotify Premium")
		expectNoWake(t, wake, "a root interface change")
		player.props.SetMust(playerIface, "PlaybackStatus", "Playing")
		expectWake(t, wake, "PropertiesChanged on the Player interface")
	})

	t.Run("an MPRIS player appearing or quitting wakes", func(t *testing.T) {
		waitForPlayers(t, addr)
		wake := listen(t)
		other, err := dbus.Connect(addr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = other.Close() })
		requestName(t, other, "org.example.NotAPlayer")
		expectNoWake(t, wake, "a name outside org.mpris.MediaPlayer2")
		player := servePlayer(t, addr, "spotifyd.instance42", fixtureProps(t, "spotifyd_playing"))
		expectWake(t, wake, "a player appearing")
		_ = player.conn.Close()
		expectWake(t, wake, "a player quitting")
	})

	t.Run("the channel closes with the context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		wake, err := New(Dial, "", time.Now).Changes(ctx)
		if err != nil {
			t.Fatalf("Changes: %v", err)
		}
		cancel()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case _, ok := <-wake:
				if !ok {
					return
				}
			case <-deadline:
				t.Fatal("channel still open after ctx ended")
			}
		}
	})

	t.Run("no session bus", func(t *testing.T) {
		t.Setenv(envSessionBus, "")
		t.Setenv(envRuntimeDir, t.TempDir())
		_, err := New(Dial, "", time.Now).Changes(context.Background())
		requireNotAvailable(t, err, "no D-Bus session bus found")
	})
}

// listen starts the notifier for the rest of the test.
func listen(t *testing.T) <-chan struct{} {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	wake, err := New(Dial, "", time.Now).Changes(ctx)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	return wake
}

func expectWake(t *testing.T, wake <-chan struct{}, what string) {
	t.Helper()
	select {
	case _, ok := <-wake:
		if !ok {
			t.Fatalf("channel closed waiting for %s", what)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("no wake for %s", what)
	}
}

// expectNoWake gives a wrongly matched signal 100 ms to arrive; on a private
// bus it takes well under a millisecond.
func expectNoWake(t *testing.T, wake <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-wake:
		t.Fatalf("woken by %s", what)
	case <-time.After(100 * time.Millisecond):
	}
}

// waitForPlayers waits until the MPRIS players on the bus are exactly want,
// sorted, so that names released by earlier tests cannot wake a listener.
func waitForPlayers(t *testing.T, addr string, want ...string) {
	t.Helper()
	conn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	bus := &godbusBus{conn: conn}
	deadline := time.Now().Add(3 * time.Second)
	for {
		names, err := bus.ListNames(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		got := suffixes(names)
		if slices.Equal(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("players = %v, want %v", got, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// requestName retries because the bus releases the name of a closed
// connection asynchronously.
func requestName(t *testing.T, conn *dbus.Conn, name string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
		if err == nil && reply == dbus.RequestNameReplyPrimaryOwner {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("request name %s: reply %v, err %v", name, reply, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
