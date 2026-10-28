package model_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/model"
)

func ptr[T any](v T) *T { return &v }

func TestNormalize(t *testing.T) {
	t.Parallel()
	fetched := time.Date(2026, 10, 27, 17, 40, 0, 0, time.UTC)
	tests := []struct {
		name  string
		in    model.NowPlaying
		check func(t *testing.T, got model.NowPlaying)
	}{
		{
			name: "sets schema version",
			in:   model.NowPlaying{},
			check: func(t *testing.T, got model.NowPlaying) {
				if got.SchemaVersion != 1 {
					t.Errorf("SchemaVersion = %d, want 1", got.SchemaVersion)
				}
			},
		},
		{
			name: "empty strings become nil",
			in: model.NowPlaying{
				Title: ptr(""), Artist: ptr(""), Album: ptr(""), AlbumArtist: ptr(""),
				URI: ptr(""), URL: ptr(""), ArtworkURL: ptr(""), ISRC: ptr(""),
			},
			check: func(t *testing.T, got model.NowPlaying) {
				for name, p := range map[string]*string{
					"Title": got.Title, "Artist": got.Artist, "Album": got.Album,
					"AlbumArtist": got.AlbumArtist, "URI": got.URI, "URL": got.URL,
					"ArtworkURL": got.ArtworkURL, "ISRC": got.ISRC,
				} {
					if p != nil {
						t.Errorf("%s = %q, want nil", name, *p)
					}
				}
			},
		},
		{
			name: "strings are kept byte for byte",
			in:   model.NowPlaying{Title: ptr("  a\nb  ")},
			check: func(t *testing.T, got model.NowPlaying) {
				if got.Title == nil || *got.Title != "  a\nb  " {
					t.Errorf("Title = %v, want untouched", got.Title)
				}
			},
		},
		{
			name: "zero and negative numbers become nil",
			in: model.NowPlaying{
				TrackNumber: ptr(0), DiscNumber: ptr(-1), DurationMS: ptr(int64(0)),
				PositionMS: ptr(int64(-1)),
			},
			check: func(t *testing.T, got model.NowPlaying) {
				if got.TrackNumber != nil || got.DiscNumber != nil || got.DurationMS != nil || got.PositionMS != nil {
					t.Errorf("want all nil, got %+v", got)
				}
			},
		},
		{
			name: "valid numbers are kept and position zero is valid",
			in: model.NowPlaying{
				TrackNumber: ptr(4), DiscNumber: ptr(1), DurationMS: ptr(int64(238341)),
				PositionMS: ptr(int64(0)),
			},
			check: func(t *testing.T, got model.NowPlaying) {
				if *got.TrackNumber != 4 || *got.DiscNumber != 1 || *got.DurationMS != 238341 || *got.PositionMS != 0 {
					t.Errorf("numbers changed: %+v", got)
				}
			},
		},
		{
			name: "artists drop empty entries and join",
			in:   model.NowPlaying{Artists: []string{"A", "", "B"}, Artist: ptr("stale")},
			check: func(t *testing.T, got model.NowPlaying) {
				if want := []string{"A", "B"}; !reflect.DeepEqual(got.Artists, want) {
					t.Errorf("Artists = %v, want %v", got.Artists, want)
				}
				if got.Artist == nil || *got.Artist != "A, B" {
					t.Errorf("Artist = %v, want %q", got.Artist, "A, B")
				}
			},
		},
		{
			name: "no artists gives empty slice and nil artist",
			in:   model.NowPlaying{Artists: []string{"", ""}, Artist: ptr("stale")},
			check: func(t *testing.T, got model.NowPlaying) {
				if got.Artists == nil || len(got.Artists) != 0 {
					t.Errorf("Artists = %#v, want empty non-nil", got.Artists)
				}
				if got.Artist != nil {
					t.Errorf("Artist = %q, want nil", *got.Artist)
				}
			},
		},
		{
			name: "artist names containing commas are not split",
			in:   model.NowPlaying{Artists: []string{"Tyler, The Creator"}},
			check: func(t *testing.T, got model.NowPlaying) {
				if len(got.Artists) != 1 || *got.Artist != "Tyler, The Creator" {
					t.Errorf("got %v / %v", got.Artists, got.Artist)
				}
			},
		},
		{
			name: "type derived from uri",
			in:   model.NowPlaying{URI: ptr("spotify:episode:abc")},
			check: func(t *testing.T, got model.NowPlaying) {
				if got.Type != model.ItemEpisode {
					t.Errorf("Type = %q, want episode", got.Type)
				}
			},
		},
		{
			name: "backend type wins over uri",
			in:   model.NowPlaying{Type: model.ItemAd, URI: ptr("spotify:track:abc")},
			check: func(t *testing.T, got model.NowPlaying) {
				if got.Type != model.ItemAd {
					t.Errorf("Type = %q, want ad", got.Type)
				}
			},
		},
		{
			name: "no uri gives unknown type",
			in:   model.NowPlaying{},
			check: func(t *testing.T, got model.NowPlaying) {
				if got.Type != model.ItemUnknown {
					t.Errorf("Type = %q, want unknown", got.Type)
				}
			},
		},
		{
			name: "url derived from track uri",
			in:   model.NowPlaying{URI: ptr("spotify:track:4brK3gpPjXArNUlU1hBXTe")},
			check: func(t *testing.T, got model.NowPlaying) {
				want := "https://open.spotify.com/track/4brK3gpPjXArNUlU1hBXTe"
				if got.URL == nil || *got.URL != want {
					t.Errorf("URL = %v, want %q", got.URL, want)
				}
			},
		},
		{
			name: "empty url is derived",
			in:   model.NowPlaying{URI: ptr("spotify:episode:abc"), URL: ptr("")},
			check: func(t *testing.T, got model.NowPlaying) {
				if got.URL == nil || *got.URL != "https://open.spotify.com/episode/abc" {
					t.Errorf("URL = %v", got.URL)
				}
			},
		},
		{
			name: "backend url is kept",
			in:   model.NowPlaying{URI: ptr("spotify:track:abc"), URL: ptr("https://open.spotify.com/track/abc?si=1")},
			check: func(t *testing.T, got model.NowPlaying) {
				if *got.URL != "https://open.spotify.com/track/abc?si=1" {
					t.Errorf("URL = %q", *got.URL)
				}
			},
		},
		{
			name: "ad and local uris have no url",
			in:   model.NowPlaying{URI: ptr("spotify:local:a:b:c:100")},
			check: func(t *testing.T, got model.NowPlaying) {
				if got.URL != nil {
					t.Errorf("URL = %q, want nil", *got.URL)
				}
				if got.Type != model.ItemTrack {
					t.Errorf("Type = %q, want track", got.Type)
				}
			},
		},
		{
			name: "fetched_at truncates to utc seconds",
			in:   model.NowPlaying{FetchedAt: time.Date(2026, 10, 27, 19, 40, 0, 987, time.FixedZone("x", 7200))},
			check: func(t *testing.T, got model.NowPlaying) {
				if !got.FetchedAt.Equal(fetched) || got.FetchedAt.Location() != time.UTC || got.FetchedAt.Nanosecond() != 0 {
					t.Errorf("FetchedAt = %v, want %v UTC", got.FetchedAt, fetched)
				}
			},
		},
		{
			name: "device is copied",
			in:   model.NowPlaying{Device: &model.Device{Name: "Mac", Type: "Computer", VolumePercent: ptr(50)}},
			check: func(t *testing.T, got model.NowPlaying) {
				if got.Device == nil || got.Device.Name != "Mac" || *got.Device.VolumePercent != 50 {
					t.Errorf("Device = %+v", got.Device)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.check(t, model.Normalize(tc.in))
		})
	}
}

func TestNormalizeDoesNotMutateInput(t *testing.T) {
	t.Parallel()
	title := ""
	in := model.NowPlaying{Title: &title, Artists: []string{"A", ""}}
	_ = model.Normalize(in)
	if in.Title == nil || len(in.Artists) != 2 || in.Artists[1] != "" {
		t.Errorf("input mutated: %+v", in)
	}
}

func TestParseURI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		uri      string
		wantType model.ItemType
		wantID   string
		wantOK   bool
	}{
		{"spotify:track:4brK3gpPjXArNUlU1hBXTe", model.ItemTrack, "4brK3gpPjXArNUlU1hBXTe", true},
		{"spotify:episode:abc", model.ItemEpisode, "abc", true},
		{"spotify:ad:xyz", model.ItemAd, "xyz", true},
		{"spotify:local:a:b:c:100", model.ItemTrack, "a:b:c:100", true},
		{"spotify:album:abc", model.ItemUnknown, "abc", true},
		{"spotify:track:", "", "", false},
		{"spotify:track", "", "", false},
		{"track:abc", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.uri, func(t *testing.T) {
			t.Parallel()
			gotType, gotID, ok := model.ParseURI(tc.uri)
			if gotType != tc.wantType || gotID != tc.wantID || ok != tc.wantOK {
				t.Errorf("ParseURI(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.uri, gotType, gotID, ok, tc.wantType, tc.wantID, tc.wantOK)
			}
		})
	}
}

func TestOpenURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		uri    string
		want   string
		wantOK bool
	}{
		{"spotify:track:4brK3gpPjXArNUlU1hBXTe", "https://open.spotify.com/track/4brK3gpPjXArNUlU1hBXTe", true},
		{"spotify:episode:abc123", "https://open.spotify.com/episode/abc123", true},
		{"spotify:ad:abc", "", false},
		{"spotify:local:abc", "", false},
		{"spotify:album:abc", "", false},
		{"spotify:track:ab-c", "", false},
		{"spotify:track:ab:c", "", false},
		{"nonsense", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.uri, func(t *testing.T) {
			t.Parallel()
			got, ok := model.OpenURL(tc.uri)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("OpenURL(%q) = (%q, %v), want (%q, %v)", tc.uri, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

const dataModelExample = `{"schema_version":1,"source":"macos","state":"playing","type":"track","title":"Amazone","artists":["Alvan"],"artist":"Alvan","album":"Home","album_artist":"Alvan","track_number":4,"disc_number":1,"duration_ms":238341,"position_ms":120165,"uri":"spotify:track:4brK3gpPjXArNUlU1hBXTe","url":"https://open.spotify.com/track/4brK3gpPjXArNUlU1hBXTe","artwork_url":"https://i.scdn.co/image/ab67616d0000b273a9ab54d90005cfd439f821e8","isrc":null,"explicit":null,"device":null,"fetched_at":"2026-10-27T17:40:00Z"}`

func TestMarshalKeyOrder(t *testing.T) {
	t.Parallel()
	raw := model.NowPlaying{
		Source: model.SourceMacOS, State: model.StatePlaying,
		Title: ptr("Amazone"), Artists: []string{"Alvan"}, Album: ptr("Home"), AlbumArtist: ptr("Alvan"),
		TrackNumber: ptr(4), DiscNumber: ptr(1), DurationMS: ptr(int64(238341)), PositionMS: ptr(int64(120165)),
		URI:        ptr("spotify:track:4brK3gpPjXArNUlU1hBXTe"),
		ArtworkURL: ptr("https://i.scdn.co/image/ab67616d0000b273a9ab54d90005cfd439f821e8"),
		FetchedAt:  time.Date(2026, 10, 27, 17, 40, 0, 123456789, time.UTC),
	}
	got, err := json.Marshal(model.Normalize(raw))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != dataModelExample {
		t.Errorf("JSON mismatch\n got: %s\nwant: %s", got, dataModelExample)
	}
}

func TestMarshalEmptyArtistsIsArray(t *testing.T) {
	t.Parallel()
	for name, np := range map[string]model.NowPlaying{
		"zero value": {},
		"normalized": model.Normalize(model.NowPlaying{}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := json.Marshal(np)
			if err != nil {
				t.Fatal(err)
			}
			var m map[string]json.RawMessage
			if err := json.Unmarshal(got, &m); err != nil {
				t.Fatal(err)
			}
			if string(m["artists"]) != "[]" {
				t.Errorf("artists = %s, want []", m["artists"])
			}
		})
	}
}
