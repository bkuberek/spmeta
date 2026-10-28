package format_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/model"
)

var update = flag.Bool("update", false, "rewrite golden files")

type fixture struct {
	name string
	np   *model.NowPlaying
}

func ptr[T any](v T) *T { return &v }

var fetchedAt = time.Date(2026, 10, 27, 17, 40, 0, 0, time.UTC)

// base is the data-model.md example; fixtures derive from it.
func base() model.NowPlaying {
	return model.NowPlaying{
		SchemaVersion: 1,
		Source:        model.SourceMacOS,
		State:         model.StatePlaying,
		Type:          model.ItemTrack,
		Title:         ptr("Amazone"),
		Artists:       []string{"Alvan"},
		Artist:        ptr("Alvan"),
		Album:         ptr("Home"),
		AlbumArtist:   ptr("Alvan"),
		TrackNumber:   ptr(4),
		DiscNumber:    ptr(1),
		DurationMS:    ptr(int64(238341)),
		PositionMS:    ptr(int64(120165)),
		URI:           ptr("spotify:track:4brK3gpPjXArNUlU1hBXTe"),
		URL:           ptr("https://open.spotify.com/track/4brK3gpPjXArNUlU1hBXTe"),
		ArtworkURL:    ptr("https://i.scdn.co/image/ab67616d0000b273a9ab54d90005cfd439f821e8"),
		FetchedAt:     fetchedAt,
	}
}

func derive(fn func(np *model.NowPlaying)) *model.NowPlaying {
	np := base()
	fn(&np)
	return &np
}

func fixtures() []fixture {
	return []fixture{
		{"playing_full", derive(func(*model.NowPlaying) {})},
		{"web_paused_device", derive(func(np *model.NowPlaying) {
			np.Source = model.SourceWeb
			np.State = model.StatePaused
			np.Title = ptr("Blinding Lights")
			np.Artists = []string{"The Weeknd"}
			np.Artist = ptr("The Weeknd")
			np.Album = ptr("After Hours")
			np.AlbumArtist = ptr("The Weeknd")
			np.DurationMS = ptr(int64(200040))
			np.PositionMS = ptr(int64(61000))
			np.ISRC = ptr("USUG11904206")
			np.Explicit = ptr(false)
			np.Device = &model.Device{Name: "Kitchen", Type: "Speaker", VolumePercent: ptr(40)}
		})},
		{"mpris_stopped", derive(func(np *model.NowPlaying) {
			np.Source = model.SourceMPRIS
			np.State = model.StateStopped
			np.PositionMS = nil
		})},
		{"no_artist", derive(func(np *model.NowPlaying) {
			np.Artists = []string{}
			np.Artist = nil
			np.AlbumArtist = nil
		})},
		{"no_album", derive(func(np *model.NowPlaying) { np.Album = nil })},
		{"duration_only", derive(func(np *model.NowPlaying) { np.PositionMS = nil })},
		{"position_only", derive(func(np *model.NowPlaying) { np.DurationMS = nil })},
		{"no_title", derive(func(np *model.NowPlaying) { np.Title = nil })},
		{"ad_no_title", derive(func(np *model.NowPlaying) {
			np.Type = model.ItemAd
			np.Title = nil
			np.Artists = []string{}
			np.Artist = nil
			np.Album = nil
			np.AlbumArtist = nil
			np.URI = ptr("spotify:ad:000000000000")
			np.URL = nil
			np.ArtworkURL = nil
			np.DurationMS = ptr(int64(30000))
			np.PositionMS = ptr(int64(5000))
		})},
		{"ad_with_title", derive(func(np *model.NowPlaying) {
			np.Type = model.ItemAd
			np.Title = ptr("Spotify")
			np.Artists = []string{}
			np.Artist = nil
			np.Album = nil
			np.AlbumArtist = nil
			np.URI = ptr("spotify:ad:000000000000")
			np.URL = nil
			np.DurationMS = ptr(int64(30000))
			np.PositionMS = ptr(int64(5000))
		})},
		{"episode_hour", derive(func(np *model.NowPlaying) {
			np.Source = model.SourceWeb
			np.Type = model.ItemEpisode
			np.Title = ptr("Episode 12")
			np.Artists = []string{"Some Publisher"}
			np.Artist = ptr("Some Publisher")
			np.Album = ptr("Some Show")
			np.AlbumArtist = nil
			np.TrackNumber = nil
			np.DiscNumber = nil
			np.URI = ptr("spotify:episode:512ojhOuo1ktJprKbVcKyQ")
			np.URL = ptr("https://open.spotify.com/episode/512ojhOuo1ktJprKbVcKyQ")
			np.DurationMS = ptr(int64(3725000))
			np.PositionMS = ptr(int64(3600000))
		})},
		{"control_chars", derive(func(np *model.NowPlaying) {
			np.Title = ptr("Line1\nLine2\tTab")
			np.Artists = []string{"A\r\nB"}
			np.Artist = ptr("A\r\nB")
			np.Album = ptr("Al\x07bum\u0085")
			np.DurationMS = ptr(int64(2000))
			np.PositionMS = ptr(int64(1000))
		})},
		{"all_null", &model.NowPlaying{
			SchemaVersion: 1,
			Source:        model.SourceMPRIS,
			State:         model.StateStopped,
			Type:          model.ItemUnknown,
			Artists:       []string{},
			FetchedAt:     fetchedAt,
		}},
	}
}

func byName(t *testing.T, name string) *model.NowPlaying {
	t.Helper()
	for _, f := range fixtures() {
		if f.name == name {
			return f.np
		}
	}
	t.Fatalf("no fixture %q", name)
	return nil
}

// checkGolden compares got with testdata/<path>, or rewrites it under -update.
func checkGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	file := filepath.Join("testdata", path)
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
		t.Errorf("%s mismatch\n got: %q\nwant: %q", file, got, want)
	}
}
