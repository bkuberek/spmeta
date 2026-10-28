package mpris

import (
	"context"
	"errors"
	"flag"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite golden files")

var fetchedAt = time.Date(2026, 10, 27, 17, 40, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

const (
	trackID    = "4brK3gpPjXArNUlU1hBXTe"
	episodeID  = "512ojhOuo1ktJprKbVcKyQ"
	artSpotify = "https://open.spotify.com/image/ab67616d0000b273a9ab54d90005cfd439f821e8"
	artCDN     = "https://i.scdn.co/image/ab67616d0000b273a9ab54d90005cfd439f821e8"
)

// metadata builds a Metadata map for the Amazone fixture track, then applies
// overrides. A nil override value deletes the key.
func metadata(overrides map[string]any) map[string]any {
	m := map[string]any{
		"xesam:title":       "Amazone",
		"xesam:artist":      []string{"Alvan"},
		"xesam:album":       "Home",
		"xesam:albumArtist": []string{"Alvan"},
		"xesam:trackNumber": int32(4),
		"xesam:discNumber":  int32(1),
		"mpris:length":      int64(238341000),
		"mpris:artUrl":      artSpotify,
	}
	for k, v := range overrides {
		if v == nil {
			delete(m, k)
			continue
		}
		m[k] = v
	}
	return m
}

func props(status string, position int64, meta map[string]any) Properties {
	return Properties{"PlaybackStatus": status, "Position": position, "Metadata": meta}
}

type fixture struct {
	name   string // golden file name and subtest name
	suffix string // bus name suffix
	props  Properties
}

// goldenFixtures are read through a fake bus and compared with testdata/<name>.json.
func goldenFixtures() []fixture {
	return []fixture{
		{"spotify_desktop_playing", "spotify", props("Playing", 0, metadata(map[string]any{
			"mpris:trackid": "/com/spotify/track/" + trackID,
		}))},
		{"spotify_desktop_string_trackid", "spotify", props("Playing", 0, metadata(map[string]any{
			"mpris:trackid": "spotify:track:" + trackID,
		}))},
		{"spotify_desktop_episode", "spotify", props("Playing", 0, metadata(map[string]any{
			"mpris:trackid":     "/com/spotify/episode/" + episodeID,
			"xesam:title":       "Episode 42",
			"xesam:artist":      []string{"The Show"},
			"xesam:album":       "The Show",
			"xesam:albumArtist": nil,
			"xesam:trackNumber": nil,
			"xesam:discNumber":  nil,
			"mpris:length":      int64(3725000000),
		}))},
		{"spotify_desktop_ad", "spotify", props("Playing", 0, metadata(map[string]any{
			"mpris:trackid":     "spotify:ad:0123456789abcdef0123456789abcdef",
			"xesam:title":       "Advertisement",
			"xesam:artist":      []string{},
			"xesam:album":       nil,
			"xesam:albumArtist": nil,
			"xesam:trackNumber": nil,
			"xesam:discNumber":  nil,
			"mpris:artUrl":      nil,
			"mpris:length":      int64(30000000),
		}))},
		{"spotify_desktop_local_file", "spotify", props("Playing", 0, metadata(map[string]any{
			"mpris:trackid":     "/com/spotify/local/0f3a9c",
			"xesam:url":         "file:///home/me/Music/Alvan%20-%20Amazone.mp3",
			"xesam:albumArtist": nil,
			"mpris:artUrl":      nil,
		}))},
		{"spotifyd_playing", "spotifyd.instance1234", props("Playing", 120165000, metadata(map[string]any{
			"mpris:trackid": "/spotify/track/" + trackID,
			"mpris:artUrl":  artCDN,
		}))},
		{"ncspot_playing", "ncspot.instance99", props("Playing", 120165000, metadata(map[string]any{
			"mpris:trackid": "/org/ncspot/spotify/track/" + trackID,
			"xesam:url":     "https://open.spotify.com/track/" + trackID,
			"mpris:artUrl":  artCDN,
		}))},
		{"spotify_player_no_trackid", "spotify_player", props("Playing", 120165000, metadata(map[string]any{
			"xesam:url":    "https://open.spotify.com/track/" + trackID + "?si=abc123",
			"mpris:artUrl": artCDN,
		}))},
		{"paused", "spotifyd.instance1234", props("Paused", 61000000, metadata(map[string]any{
			"mpris:trackid": "/spotify/track/" + trackID,
		}))},
		{"stopped_with_item", "spotifyd.instance1234", props("Stopped", 0, metadata(map[string]any{
			"mpris:trackid": "/spotify/track/" + trackID,
		}))},
	}
}

// nothingFixtures must map to a nothing-playing error.
func nothingFixtures() []fixture {
	return []fixture{
		{"noTrack", "spotify", props("Playing", 0, map[string]any{"mpris:trackid": noTrackPath})},
		{"noTrackWithTitle", "spotify", props("Paused", 0, metadata(map[string]any{"mpris:trackid": noTrackPath}))},
		{"emptyMetadata", "spotifyd.instance1", props("Paused", 0, map[string]any{})},
		{"missingMetadata", "spotifyd.instance1", Properties{"PlaybackStatus": "Paused"}},
		{"stoppedEmpty", "spotifyd.instance1", props("Stopped", 0, map[string]any{"mpris:length": int64(0)})},
	}
}

// fakeBus implements Bus without a session bus.
type fakeBus struct {
	names      []string
	namesErr   error
	blockNames bool
	players    map[string]fakePlayer // by suffix
	getCalls   []string
	closed     atomic.Int32
}

type fakePlayer struct {
	props Properties
	err   error
	block bool
}

func newFakeBus(players map[string]fakePlayer, extraNames ...string) *fakeBus {
	names := slices.Sorted(maps.Keys(players))
	for i, s := range names {
		names[i] = busPrefix + s
	}
	return &fakeBus{names: append(extraNames, names...), players: players}
}

func (f *fakeBus) ListNames(ctx context.Context) ([]string, error) {
	if f.blockNames {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.names, f.namesErr
}

func (f *fakeBus) GetAll(ctx context.Context, busName string) (Properties, error) {
	suffix, ok := strings.CutPrefix(busName, busPrefix)
	if !ok {
		return nil, errors.New("not an MPRIS bus name: " + busName)
	}
	f.getCalls = append(f.getCalls, suffix)
	p, ok := f.players[suffix]
	if !ok {
		return nil, errors.New("unknown bus name " + busName)
	}
	if p.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return p.props, p.err
}

func (f *fakeBus) Close() error {
	f.closed.Add(1)
	return nil
}

func ok(p Properties) fakePlayer { return fakePlayer{props: p} }

func dialer(b Bus) DialFunc {
	return func(context.Context) (Bus, error) { return b, nil }
}

func newBackend(b Bus, player string) *Backend {
	return New(dialer(b), player, func() time.Time { return fetchedAt })
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	file := filepath.Join("testdata", name+".json")
	if *update {
		if err := os.WriteFile(file, got, 0o644); err != nil {
			t.Fatalf("update %s: %v", file, err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(want) != string(got) {
		t.Errorf("%s mismatch\n got: %s\nwant: %s", file, got, want)
	}
}
