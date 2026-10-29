package web

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

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

func TestGoldenJSONValidatesAgainstSchema(t *testing.T) {
	s := compileSchema(t)
	files, err := filepath.Glob(filepath.Join("testdata", "golden", "*.json"))
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

// readBody serves body as a 200 response and returns what Read makes of it.
func readBody(t *testing.T, body string) (model.NowPlaying, error) {
	t.Helper()
	return newRig(newScript(t, ok([]byte(body)))).Read(context.Background())
}

func mustRead(t *testing.T, body string) model.NowPlaying {
	t.Helper()
	np, err := readBody(t, body)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return np
}

func TestMapTrack(t *testing.T) {
	r := newRig(newScript(t, ok(fixture(t, "player-track.json"))))
	got, err := r.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := model.NowPlaying{
		SchemaVersion: 1,
		Source:        model.SourceWeb,
		State:         model.StatePlaying,
		Type:          model.ItemTrack,
		Title:         ptr("Amazone"),
		Artists:       []string{"Alvan", "Tyler, The Creator"},
		Artist:        ptr("Alvan, Tyler, The Creator"),
		Album:         ptr("Home"),
		AlbumArtist:   ptr("Alvan, Ahmed"),
		TrackNumber:   ptr(4),
		DiscNumber:    ptr(1),
		DurationMS:    ptr(int64(238341)),
		PositionMS:    ptr(int64(120165)),
		URI:           ptr("spotify:track:4brK3gpPjXArNUlU1hBXTe"),
		URL:           ptr("https://open.spotify.com/track/4brK3gpPjXArNUlU1hBXTe"),
		ArtworkURL:    ptr("https://i.scdn.co/image/ab67616d0000b273a9ab54d90005cfd439f821e8"),
		ISRC:          ptr("FRZ111800001"),
		Explicit:      ptr(false),
		Device:        &model.Device{Name: "Living Room", Type: "Speaker", VolumePercent: ptr(55)},
		FetchedAt:     fetchedAt,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("track mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestMapPausedTrack(t *testing.T) {
	np := mustRead(t, string(fixture(t, "player-track-paused.json")))
	if np.State != model.StatePaused || np.Explicit == nil || !*np.Explicit {
		t.Errorf("state %q explicit %v, want paused and explicit", np.State, np.Explicit)
	}
}

func TestMapEpisode(t *testing.T) {
	got := mustRead(t, string(fixture(t, "player-episode.json")))
	want := model.NowPlaying{
		SchemaVersion: 1,
		Source:        model.SourceWeb,
		State:         model.StatePlaying,
		Type:          model.ItemEpisode,
		Title:         ptr("Episode 42"),
		Artists:       []string{"The Publisher"},
		Artist:        ptr("The Publisher"),
		Album:         ptr("The Show"),
		DurationMS:    ptr(int64(3725000)),
		PositionMS:    ptr(int64(1800000)),
		URI:           ptr("spotify:episode:512ojhOuo1ktJprKbVcKyQ"),
		URL:           ptr("https://open.spotify.com/episode/512ojhOuo1ktJprKbVcKyQ"),
		ArtworkURL:    ptr("https://i.scdn.co/image/ab6765630000f68d1c2d3e4f5a6b7c8d9e0f1a2b"),
		Explicit:      ptr(true),
		Device:        &model.Device{Name: "Pixel 9", Type: "Smartphone", VolumePercent: ptr(30)},
		FetchedAt:     fetchedAt,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("episode mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestEpisodeArtworkFallsBackToTheShow(t *testing.T) {
	tests := []struct {
		name   string
		images string
		want   *string
	}{
		{"item images win", `[{"url":"https://i.scdn.co/image/item","width":64,"height":64}]`, ptr("https://i.scdn.co/image/item")},
		{"empty item images use the show", `[]`, ptr("https://i.scdn.co/image/show")},
		{"missing item images use the show", ``, ptr("https://i.scdn.co/image/show")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			images := ""
			if tt.images != "" {
				images = `"images":` + tt.images + `,`
			}
			np := mustRead(t, `{"is_playing":true,"currently_playing_type":"episode","item":{"type":"episode","name":"E",`+images+
				`"uri":"spotify:episode:512ojhOuo1ktJprKbVcKyQ","show":{"name":"S","publisher":"P","images":[{"url":"https://i.scdn.co/image/show","width":300,"height":300}]}}}`)
			if !reflect.DeepEqual(np.ArtworkURL, tt.want) {
				t.Errorf("artwork = %v, want %v", deref(np.ArtworkURL), deref(tt.want))
			}
		})
	}
}

func TestEpisodeWithoutShow(t *testing.T) {
	np := mustRead(t, `{"is_playing":true,"item":{"type":"episode","name":"E","uri":"spotify:episode:512ojhOuo1ktJprKbVcKyQ"}}`)
	if len(np.Artists) != 0 || np.Artist != nil || np.Album != nil || np.ArtworkURL != nil {
		t.Errorf("artists %v artist %v album %v artwork %v, want all empty", np.Artists, deref(np.Artist), deref(np.Album), deref(np.ArtworkURL))
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func TestMapAdWithNullItem(t *testing.T) {
	got := mustRead(t, string(fixture(t, "player-ad.json")))
	want := model.NowPlaying{
		SchemaVersion: 1,
		Source:        model.SourceWeb,
		State:         model.StatePlaying,
		Type:          model.ItemAd,
		Artists:       []string{},
		PositionMS:    ptr(int64(4500)),
		Device:        &model.Device{Name: "Living Room", Type: "Speaker", VolumePercent: ptr(55)},
		FetchedAt:     fetchedAt,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ad mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestAdStateFollowsIsPlaying(t *testing.T) {
	np := mustRead(t, `{"is_playing":false,"currently_playing_type":"ad","item":null}`)
	if np.State != model.StatePaused || np.Type != model.ItemAd {
		t.Errorf("state %q type %q, want paused ad", np.State, np.Type)
	}
}

func TestAdWithAnItemMapsTheItem(t *testing.T) {
	np := mustRead(t, `{"is_playing":true,"currently_playing_type":"ad","item":{"type":"track","name":"Sponsored","uri":"spotify:track:4brK3gpPjXArNUlU1hBXTe","duration_ms":30000}}`)
	if np.Type != model.ItemAd || np.Title == nil || *np.Title != "Sponsored" || np.DurationMS == nil || *np.DurationMS != 30000 {
		t.Errorf("type %q title %v duration %v, want an ad carrying the item", np.Type, deref(np.Title), np.DurationMS)
	}
}

func TestMapLocalFile(t *testing.T) {
	got := mustRead(t, string(fixture(t, "player-local-file.json")))
	want := model.NowPlaying{
		SchemaVersion: 1,
		Source:        model.SourceWeb,
		State:         model.StatePlaying,
		Type:          model.ItemTrack,
		Title:         ptr("Amazone"),
		Artists:       []string{"Alvan"},
		Artist:        ptr("Alvan"),
		Album:         ptr("Home"),
		AlbumArtist:   ptr("Alvan"),
		DurationMS:    ptr(int64(238000)),
		PositionMS:    ptr(int64(12000)),
		URI:           ptr("spotify:local:Alvan:Home:Amazone:238"),
		Explicit:      ptr(false),
		Device:        &model.Device{Name: "MacBook Pro", Type: "Computer", VolumePercent: ptr(80)},
		FetchedAt:     fetchedAt,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("local file mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestMapTrackWithoutISRCAndVolume(t *testing.T) {
	np := mustRead(t, string(fixture(t, "player-no-isrc.json")))
	if np.ISRC != nil {
		t.Errorf("isrc = %q, want null", *np.ISRC)
	}
	if np.Device == nil || np.Device.VolumePercent != nil {
		t.Errorf("device = %+v, want a device with null volume", np.Device)
	}
}

func TestMapUnknownItemType(t *testing.T) {
	np := mustRead(t, `{"is_playing":true,"progress_ms":5,"currently_playing_type":"unknown","item":{"type":"chapter","name":"Chapter 1","uri":"spotify:chapter:abc","duration_ms":1000,
		"artists":[{"name":"Ignored"}],"album":{"name":"Ignored"},"explicit":true,"external_ids":{"isrc":"X"},"external_urls":{"spotify":"https://open.spotify.com/chapter/abc"},"track_number":3}}`)
	want := model.NowPlaying{
		SchemaVersion: 1,
		Source:        model.SourceWeb,
		State:         model.StatePlaying,
		Type:          model.ItemUnknown,
		Artists:       []string{},
		Title:         ptr("Chapter 1"),
		DurationMS:    ptr(int64(1000)),
		PositionMS:    ptr(int64(5)),
		URI:           ptr("spotify:chapter:abc"),
		FetchedAt:     fetchedAt,
	}
	if !reflect.DeepEqual(np, want) {
		t.Errorf("unknown item mismatch\n got: %+v\nwant: %+v", np, want)
	}
}

func TestMapSparseTrack(t *testing.T) {
	// Unknown fields are ignored; absent ones become null, and Normalize
	// derives the URL from the URI.
	np := mustRead(t, `{"is_playing":true,"future_field":{"a":1},"item":{"type":"track","name":"T","uri":"spotify:track:4brK3gpPjXArNUlU1hBXTe","new":1}}`)
	if np.Type != model.ItemTrack || np.URL == nil || *np.URL != "https://open.spotify.com/track/4brK3gpPjXArNUlU1hBXTe" {
		t.Errorf("type %q url %v, want a track with a derived URL", np.Type, deref(np.URL))
	}
	if np.PositionMS != nil || np.DurationMS != nil || np.Device != nil || np.Explicit != nil || np.AlbumArtist != nil {
		t.Errorf("position %v duration %v device %v explicit %v album_artist %v, want all null", np.PositionMS, np.DurationMS, np.Device, np.Explicit, deref(np.AlbumArtist))
	}
}

func TestTrackURLPrefersExternalURLs(t *testing.T) {
	np := mustRead(t, `{"is_playing":true,"item":{"type":"track","name":"T","uri":"spotify:track:4brK3gpPjXArNUlU1hBXTe","external_urls":{"spotify":"https://open.spotify.com/track/4brK3gpPjXArNUlU1hBXTe?si=x"}}}`)
	if got := deref(np.URL); got != "https://open.spotify.com/track/4brK3gpPjXArNUlU1hBXTe?si=x" {
		t.Errorf("url = %q, want the external_urls value", got)
	}
}

func TestLargestImage(t *testing.T) {
	img := func(url string, w, h any) string {
		return fmt.Sprintf(`{"url":"https://i.scdn.co/image/%s","width":%v,"height":%v}`, url, w, h)
	}
	tests := []struct {
		name   string
		images []string
		want   string
	}{
		{"largest listed last", []string{img("s", 64, 64), img("m", 300, 300), img("l", 640, 640)}, "l"},
		{"largest listed first", []string{img("l", 640, 640), img("s", 64, 64)}, "l"},
		{"largest listed in the middle", []string{img("s", 64, 64), img("l", 640, 640), img("m", 300, 300)}, "l"},
		{"area, not width", []string{img("wide", 1000, 100), img("square", 400, 400)}, "square"},
		{"equal areas keep the first", []string{img("a", 300, 300), img("b", 300, 300)}, "a"},
		{"no dimensions takes the first", []string{img("first", "null", "null"), img("second", "null", "null")}, "first"},
		{"missing dimensions take the first", []string{`{"url":"https://i.scdn.co/image/first"}`, `{"url":"https://i.scdn.co/image/second"}`}, "first"},
		{"dimensions beat none", []string{img("none", "null", "null"), img("sized", 64, 64)}, "sized"},
		{"one dimension only counts as none", []string{img("half", 640, "null"), img("sized", 64, 64)}, "sized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			np := mustRead(t, `{"is_playing":true,"item":{"type":"track","name":"T","uri":"spotify:track:4brK3gpPjXArNUlU1hBXTe","album":{"images":[`+strings.Join(tt.images, ",")+`]}}}`)
			if want := "https://i.scdn.co/image/" + tt.want; deref(np.ArtworkURL) != want {
				t.Errorf("artwork = %q, want %q", deref(np.ArtworkURL), want)
			}
		})
	}
}

func TestImagesThatCannotBeArtwork(t *testing.T) {
	tests := []struct {
		name   string
		images string
		want   string
	}{
		{"none", `[]`, "<nil>"},
		{"null", `null`, "<nil>"},
		{"empty url", `[{"url":"","width":640,"height":640},{"url":"https://i.scdn.co/image/ok","width":64,"height":64}]`, "https://i.scdn.co/image/ok"},
		{"not https", `[{"url":"http://i.scdn.co/image/plain","width":640,"height":640},{"url":"https://i.scdn.co/image/ok","width":64,"height":64}]`, "https://i.scdn.co/image/ok"},
		{"only not https", `[{"url":"http://i.scdn.co/image/plain","width":640,"height":640}]`, "<nil>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			np := mustRead(t, `{"is_playing":true,"item":{"type":"track","name":"T","uri":"spotify:track:4brK3gpPjXArNUlU1hBXTe","album":{"images":`+tt.images+`}}}`)
			if got := deref(np.ArtworkURL); got != tt.want {
				t.Errorf("artwork = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMapZeroValuesBecomeNull(t *testing.T) {
	np := mustRead(t, `{"is_playing":true,"progress_ms":-1,"item":{"type":"track","name":"","uri":"spotify:track:4brK3gpPjXArNUlU1hBXTe","duration_ms":0,"track_number":0,"disc_number":0,"artists":[{"name":""},{"name":"A"}]}}`)
	if np.Title != nil || np.DurationMS != nil || np.TrackNumber != nil || np.DiscNumber != nil || np.PositionMS != nil {
		t.Errorf("title %v duration %v track %v disc %v position %v, want all null", deref(np.Title), np.DurationMS, np.TrackNumber, np.DiscNumber, np.PositionMS)
	}
	if !reflect.DeepEqual(np.Artists, []string{"A"}) {
		t.Errorf("artists = %q, want the empty entry dropped", np.Artists)
	}
}
