package mpris

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
)

func TestMatches(t *testing.T) {
	tests := []struct {
		name, suffix string
		want         bool
	}{
		{"spotify", "spotify", true},
		{"spotify", "spotify.instance1", true},
		{"spotify", "spotifyd", false},
		{"spotify", "spotifyx", false},
		{"spotifyd", "spotifyd.instance1234", true},
		{"spotifyd", "spotify", false},
		{"firefox", "firefox.instance123", true},
		{"firefox.instance123", "firefox.instance123", true},
		{"firefox.instance123", "firefox.instance1234", false},
		{"firefox.instance1", "firefox.instance12", false},
		{"spotify", "Spotify", false},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.suffix, func(t *testing.T) {
			if got := matches(tt.name, tt.suffix); got != tt.want {
				t.Errorf("matches(%q, %q) = %v, want %v", tt.name, tt.suffix, got, tt.want)
			}
		})
	}
}

func TestSuffixes(t *testing.T) {
	got := suffixes([]string{
		":1.42",
		"org.freedesktop.DBus",
		"org.mpris.MediaPlayer2.spotifyd.instance9",
		"org.mpris.MediaPlayer2.spotify",
		"org.mpris.MediaPlayer2.spotify",
		"org.mpris.MediaPlayer2.",
		"org.mpris.MediaPlayer2.firefox.instance1",
	})
	want := []string{"firefox.instance1", "spotify", "spotifyd.instance9"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("suffixes = %v, want %v", got, want)
	}
}

func playerProps(status string) Properties {
	return props(status, 0, metadata(map[string]any{"mpris:trackid": "/spotify/track/" + trackID}))
}

func selectedSuffix(t *testing.T, bus *fakeBus, player string) (string, error) {
	t.Helper()
	b := newBackend(bus, player)
	sel, err := choose(context.Background(), suffixes(bus.names), player, b.getter(bus))
	return sel.suffix, err
}

func TestChoose(t *testing.T) {
	tests := []struct {
		name    string
		players map[string]fakePlayer
		extra   []string
		player  string
		want    string
	}{
		{
			name:    "browsers are ignored by default",
			players: map[string]fakePlayer{"firefox.instance1": ok(playerProps("Playing")), "spotify": ok(playerProps("Paused"))},
			want:    "spotify",
		},
		{
			name:    "spotify beats spotifyd even when paused",
			players: map[string]fakePlayer{"spotify": ok(playerProps("Paused")), "spotifyd.instance1": ok(playerProps("Playing"))},
			want:    "spotify",
		},
		{
			name: "preference order",
			players: map[string]fakePlayer{
				"spotify_player": ok(playerProps("Playing")),
				"ncspot":         ok(playerProps("Playing")),
				"spotifyd":       ok(playerProps("Playing")),
			},
			want: "spotifyd",
		},
		{
			name: "playing wins within an entry",
			players: map[string]fakePlayer{
				"spotifyd.instance1": ok(playerProps("Paused")),
				"spotifyd.instance9": ok(playerProps("Playing")),
			},
			want: "spotifyd.instance9",
		},
		{
			name: "sorted tie-break when none is playing",
			players: map[string]fakePlayer{
				"spotifyd.instance9":  ok(playerProps("Paused")),
				"spotifyd.instance10": ok(playerProps("Paused")),
			},
			want: "spotifyd.instance10",
		},
		{
			name: "sorted tie-break when several are playing",
			players: map[string]fakePlayer{
				"spotifyd.instance9":  ok(playerProps("Playing")),
				"spotifyd.instance10": ok(playerProps("Playing")),
			},
			want: "spotifyd.instance10",
		},
		{
			name: "failing GetAll is skipped",
			players: map[string]fakePlayer{
				"spotify":            {err: errors.New("boom")},
				"spotifyd.instance1": ok(playerProps("Paused")),
			},
			want: "spotifyd.instance1",
		},
		{
			name: "failing instance is skipped within an entry",
			players: map[string]fakePlayer{
				"spotifyd.instance1": {err: errors.New("boom")},
				"spotifyd.instance2": ok(playerProps("Paused")),
			},
			want: "spotifyd.instance2",
		},
		{
			name:    "player flag selects a browser",
			players: map[string]fakePlayer{"firefox.instance123": ok(playerProps("Playing")), "spotify": ok(playerProps("Playing"))},
			player:  "firefox",
			want:    "firefox.instance123",
		},
		{
			name: "player flag with a full suffix",
			players: map[string]fakePlayer{
				"firefox.instance1":   ok(playerProps("Playing")),
				"firefox.instance123": ok(playerProps("Paused")),
			},
			player: "firefox.instance123",
			want:   "firefox.instance123",
		},
		{
			name:    "player flag overrides the preference list",
			players: map[string]fakePlayer{"spotify": ok(playerProps("Playing")), "ncspot.instance2": ok(playerProps("Paused"))},
			player:  "ncspot",
			want:    "ncspot.instance2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectedSuffix(t, newFakeBus(tt.players, tt.extra...), tt.player)
			if err != nil {
				t.Fatalf("choose: %v", err)
			}
			if got != tt.want {
				t.Errorf("selected %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChooseErrors(t *testing.T) {
	boom := fakePlayer{err: errors.New("boom")}
	tests := []struct {
		name    string
		players map[string]fakePlayer
		player  string
		message string
	}{
		{"no players", nil, "", "no Spotify MPRIS player found"},
		{"only browsers", map[string]fakePlayer{"firefox.instance1": ok(playerProps("Playing"))}, "", "no Spotify MPRIS player found"},
		{"prefix is not a match", map[string]fakePlayer{"spotifydx": ok(playerProps("Playing")), "spotifyx": ok(playerProps("Playing"))}, "", "no Spotify MPRIS player found"},
		{"named player missing", map[string]fakePlayer{"spotify": ok(playerProps("Playing"))}, "firefox", "no MPRIS player named firefox"},
		{"named suffix is not a prefix match", map[string]fakePlayer{"firefox.instance1234": ok(playerProps("Playing"))}, "firefox.instance123", "no MPRIS player named firefox.instance123"},
		{"every GetAll fails", map[string]fakePlayer{"spotify": boom, "ncspot": boom}, "", "MPRIS players did not answer"},
		{"named player does not answer", map[string]fakePlayer{"firefox.instance1": boom}, "firefox", "MPRIS players did not answer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := selectedSuffix(t, newFakeBus(tt.players), tt.player)
			var be *backend.Error
			if !errors.As(err, &be) {
				t.Fatalf("err = %v, want *backend.Error", err)
			}
			if be.Kind != backend.KindNotAvailable || be.Message != tt.message {
				t.Errorf("got kind %v message %q, want NotAvailable %q", be.Kind, be.Message, tt.message)
			}
		})
	}
}

func TestChooseSkipsTimedOutPlayer(t *testing.T) {
	bus := newFakeBus(map[string]fakePlayer{
		"spotify":            {block: true},
		"spotifyd.instance1": ok(playerProps("Paused")),
	})
	b := newBackend(bus, "")
	b.timeouts.getAll = 20 * time.Millisecond
	sel, err := choose(context.Background(), suffixes(bus.names), "", b.getter(bus))
	if err != nil {
		t.Fatalf("choose: %v", err)
	}
	if sel.suffix != "spotifyd.instance1" {
		t.Errorf("selected %q, want spotifyd.instance1", sel.suffix)
	}
}

func TestChooseStopsWhenContextEnds(t *testing.T) {
	bus := newFakeBus(map[string]fakePlayer{"spotify": ok(playerProps("Playing"))})
	b := newBackend(bus, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := choose(ctx, suffixes(bus.names), "", b.getter(bus))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
