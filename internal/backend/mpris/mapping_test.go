package mpris

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/format"
	"github.com/bkuberek/spmeta/internal/model"
	"github.com/bkuberek/spmeta/schema"
)

const schemaID = "https://raw.githubusercontent.com/bkuberek/spmeta/main/schema/spmeta.schema.json"

func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.JSON))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource(schemaID, doc); err != nil {
		t.Fatalf("add schema: %v", err)
	}
	s, err := c.Compile(schemaID)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return s
}

func TestGoldenJSON(t *testing.T) {
	for _, f := range goldenFixtures() {
		t.Run(f.name, func(t *testing.T) {
			bus := newFakeBus(map[string]fakePlayer{f.suffix: ok(f.props)})
			np, err := newBackend(bus, "").Read(context.Background())
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			got, err := format.JSON(&np)
			if err != nil {
				t.Fatal(err)
			}
			checkGolden(t, f.name, got)
		})
	}
}

func TestGoldenJSONValidatesAgainstSchema(t *testing.T) {
	s := compileSchema(t)
	files, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < len(goldenFixtures()) {
		t.Fatalf("found %d golden files, want at least %d", len(files), len(goldenFixtures()))
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if err := s.Validate(inst); err != nil {
				t.Errorf("schema validation: %v", err)
			}
		})
	}
}

func TestNothingPlaying(t *testing.T) {
	for _, f := range nothingFixtures() {
		t.Run(f.name, func(t *testing.T) {
			bus := newFakeBus(map[string]fakePlayer{f.suffix: ok(f.props)})
			_, err := newBackend(bus, "").Read(context.Background())
			if got := backend.KindOf(err); got != backend.KindNothingPlaying {
				t.Errorf("Read error = %v (kind %v), want nothing playing", err, got)
			}
		})
	}
}

func mapOne(t *testing.T, suffix string, p Properties) model.NowPlaying {
	t.Helper()
	np, err := mapProperties(suffix, p)
	if err != nil {
		t.Fatalf("mapProperties: %v", err)
	}
	return model.Normalize(np)
}

func TestMapStates(t *testing.T) {
	tests := []struct {
		status string
		want   model.State
	}{
		{"Playing", model.StatePlaying},
		{"Paused", model.StatePaused},
		{"Stopped", model.StateStopped},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			np := mapOne(t, "spotifyd", playerProps(tt.status))
			if np.State != tt.want || np.Source != model.SourceMPRIS {
				t.Errorf("got state %q source %q, want %q mpris", np.State, np.Source, tt.want)
			}
		})
	}
}

func TestMapUnexpectedStatus(t *testing.T) {
	for _, p := range []Properties{playerProps("Buffering"), {"Metadata": metadata(nil)}} {
		_, err := mapProperties("spotifyd.instance7", p)
		var be *backend.Error
		if !errors.As(err, &be) || be.Kind != backend.KindUnexpected || be.Source != model.SourceMPRIS {
			t.Fatalf("err = %v, want Unexpected mpris error", err)
		}
		if !strings.Contains(be.Message, "PlaybackStatus") || !strings.Contains(be.Message, "spotifyd.instance7") {
			t.Errorf("message %q should name the property and the player", be.Message)
		}
	}
}

func TestMapTrackID(t *testing.T) {
	tests := []struct {
		name, trackID, want string
	}{
		{"spotify desktop path", "/com/spotify/track/" + trackID, "spotify:track:" + trackID},
		{"spotify desktop string", "spotify:track:" + trackID, "spotify:track:" + trackID},
		{"spotifyd", "/spotify/track/" + trackID, "spotify:track:" + trackID},
		{"ncspot", "/org/ncspot/spotify/track/" + trackID, "spotify:track:" + trackID},
		{"episode", "/com/spotify/episode/" + episodeID, "spotify:episode:" + episodeID},
		{"ad string", "spotify:ad:abc123", "spotify:ad:abc123"},
		{"local file path has no recoverable uri", "/com/spotify/local/0f3a9c", ""},
		{"local file string kept", "spotify:local:Alvan:Home:Amazone:238", "spotify:local:Alvan:Home:Amazone:238"},
		{"trailing slash", "/com/spotify/track/" + trackID + "/", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			np := mapOne(t, "spotify", props("Playing", 1, metadata(map[string]any{"mpris:trackid": tt.trackID})))
			if tt.want == "" {
				if np.URI != nil {
					t.Errorf("uri = %q, want null", *np.URI)
				}
				return
			}
			if np.URI == nil || *np.URI != tt.want {
				t.Errorf("uri = %v, want %q", np.URI, tt.want)
			}
		})
	}
}

func TestMapURIAndURLFromXesamURL(t *testing.T) {
	spotifyURL := "https://open.spotify.com/track/" + trackID
	tests := []struct {
		name    string
		meta    map[string]any
		wantURI string
		wantURL string
	}{
		{"trackid wins over url", map[string]any{"mpris:trackid": "/spotify/track/AAAA", "xesam:url": spotifyURL}, "spotify:track:AAAA", spotifyURL},
		{"url only", map[string]any{"xesam:url": spotifyURL}, "spotify:track:" + trackID, spotifyURL},
		{"query is kept in url but ignored for uri", map[string]any{"xesam:url": spotifyURL + "?si=x"}, "spotify:track:" + trackID, spotifyURL + "?si=x"},
		{"episode url", map[string]any{"xesam:url": "https://open.spotify.com/episode/" + episodeID}, "spotify:episode:" + episodeID, "https://open.spotify.com/episode/" + episodeID},
		{"spotify uri in url", map[string]any{"xesam:url": "spotify:track:" + trackID}, "spotify:track:" + trackID, spotifyURL},
		{"file url", map[string]any{"xesam:url": "file:///music/a.mp3"}, "", ""},
		{"other https url", map[string]any{"xesam:url": "https://example.com/track/" + trackID}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := metadata(map[string]any{"xesam:url": nil})
			for k, v := range tt.meta {
				meta[k] = v
			}
			np := mapOne(t, "ncspot", props("Playing", 1, meta))
			if got := deref(np.URI); got != tt.wantURI {
				t.Errorf("uri = %q, want %q", got, tt.wantURI)
			}
			if got := deref(np.URL); got != tt.wantURL {
				t.Errorf("url = %q, want %q", got, tt.wantURL)
			}
		})
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func TestMapArtwork(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"open.spotify.com image is rewritten", artSpotify, artCDN},
		{"query string is dropped when rewriting", artSpotify + "?x=1", artCDN},
		{"cdn url is kept", artCDN, artCDN},
		{"other https is kept", "https://example.com/cover.jpg", "https://example.com/cover.jpg"},
		{"http is null", "http://open.spotify.com/image/abc", ""},
		{"file url is null", "file:///tmp/cover.jpg", ""},
		{"empty is null", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			np := mapOne(t, "spotifyd", props("Playing", 1, metadata(map[string]any{"mpris:artUrl": tt.in})))
			if got := deref(np.ArtworkURL); got != tt.want {
				t.Errorf("artwork = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMapOffSpecValueTypes(t *testing.T) {
	tests := []struct {
		name    string
		meta    map[string]any
		pos     any
		check   func(np model.NowPlaying) bool
		message string
	}{
		{
			"artist as string",
			map[string]any{"xesam:artist": "Alvan"},
			int64(0),
			func(np model.NowPlaying) bool { return reflect.DeepEqual(np.Artists, []string{"Alvan"}) }, "artists = [Alvan]",
		},
		{
			"artist as []any",
			map[string]any{"xesam:artist": []any{"A", "B", 3}},
			int64(0),
			func(np model.NowPlaying) bool {
				return reflect.DeepEqual(np.Artists, []string{"A", "B"}) && deref(np.Artist) == "A, B"
			}, "artists = [A B]",
		},
		{
			"length as uint64",
			map[string]any{"mpris:length": uint64(238341000)},
			int64(0),
			func(np model.NowPlaying) bool { return np.DurationMS != nil && *np.DurationMS == 238341 }, "duration 238341",
		},
		{
			"length as double",
			map[string]any{"mpris:length": float64(238341999)},
			int64(0),
			func(np model.NowPlaying) bool { return np.DurationMS != nil && *np.DurationMS == 238341 }, "duration 238341",
		},
		{
			"length overflows int64",
			map[string]any{"mpris:length": uint64(1) << 63},
			int64(0),
			func(np model.NowPlaying) bool { return np.DurationMS == nil }, "duration null",
		},
		{
			"length of the wrong type",
			map[string]any{"mpris:length": "238341000"},
			int64(0),
			func(np model.NowPlaying) bool { return np.DurationMS == nil }, "duration null",
		},
		{
			"position as uint32",
			map[string]any{},
			uint32(2500000),
			func(np model.NowPlaying) bool { return np.PositionMS != nil && *np.PositionMS == 2500 }, "position 2500",
		},
		{
			"track number as int64",
			map[string]any{"xesam:trackNumber": int64(9)},
			int64(0),
			func(np model.NowPlaying) bool { return np.TrackNumber != nil && *np.TrackNumber == 9 }, "track 9",
		},
		{
			"album artist list is joined",
			map[string]any{"xesam:albumArtist": []string{"A", "B"}},
			int64(0),
			func(np model.NowPlaying) bool { return deref(np.AlbumArtist) == "A, B" }, "album artist A, B",
		},
		{
			"title of the wrong type",
			map[string]any{"xesam:title": int32(5)},
			int64(0),
			func(np model.NowPlaying) bool { return np.Title == nil }, "title null",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := props("Playing", 0, metadata(tt.meta))
			p["Position"] = tt.pos
			np := mapOne(t, "spotifyd", p)
			if !tt.check(np) {
				t.Errorf("want %s, got %+v", tt.message, np)
			}
		})
	}
}

func TestMapMissingFields(t *testing.T) {
	p := Properties{
		"PlaybackStatus": "Playing",
		"Metadata":       map[string]any{"xesam:title": "Only a title"},
	}
	np := mapOne(t, "spotifyd", p)
	if deref(np.Title) != "Only a title" {
		t.Errorf("title = %v", np.Title)
	}
	if np.Type != model.ItemUnknown {
		t.Errorf("type = %q, want unknown", np.Type)
	}
	if len(np.Artists) != 0 || np.Album != nil || np.URI != nil || np.URL != nil ||
		np.ArtworkURL != nil || np.DurationMS != nil || np.PositionMS != nil ||
		np.TrackNumber != nil || np.DiscNumber != nil {
		t.Errorf("want every other field null, got %+v", np)
	}
}

func TestMapItemTypes(t *testing.T) {
	tests := []struct {
		trackID string
		want    model.ItemType
	}{
		{"/com/spotify/track/" + trackID, model.ItemTrack},
		{"/com/spotify/episode/" + episodeID, model.ItemEpisode},
		{"spotify:ad:abc", model.ItemAd},
		{"/com/spotify/local/abc", model.ItemTrack},
		{"spotify:local:Alvan:Home:Amazone:238", model.ItemTrack},
	}
	for _, tt := range tests {
		np := mapOne(t, "spotify", props("Playing", 1, metadata(map[string]any{"mpris:trackid": tt.trackID})))
		if np.Type != tt.want {
			t.Errorf("trackid %q: type = %q, want %q", tt.trackID, np.Type, tt.want)
		}
	}
}
