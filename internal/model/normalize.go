package model

import (
	"strings"
	"time"
)

const (
	uriPrefix = "spotify:"
	urlBase   = "https://open.spotify.com/"
)

// Normalize applies the rules in docs/design/data-model.md. It returns a new
// value and does not modify the input or anything it points to.
func Normalize(np NowPlaying) NowPlaying {
	np.SchemaVersion = SchemaVersion

	np.Title = nonEmpty(np.Title)
	np.Album = nonEmpty(np.Album)
	np.AlbumArtist = nonEmpty(np.AlbumArtist)
	np.URI = nonEmpty(np.URI)
	np.URL = nonEmpty(np.URL)
	np.ArtworkURL = nonEmpty(np.ArtworkURL)
	np.ISRC = nonEmpty(np.ISRC)

	np.Artists = compact(np.Artists)
	np.Artist = nil
	if len(np.Artists) > 0 {
		joined := strings.Join(np.Artists, ", ")
		np.Artist = &joined
	}

	np.TrackNumber = positive(np.TrackNumber)
	np.DiscNumber = positive(np.DiscNumber)
	np.DurationMS = positive(np.DurationMS)
	np.PositionMS = nonNegative(np.PositionMS)

	if np.Type == "" {
		np.Type = ItemUnknown
		if np.URI != nil {
			if t, _, ok := ParseURI(*np.URI); ok {
				np.Type = t
			}
		}
	}
	if np.URL == nil && np.URI != nil {
		if u, ok := OpenURL(*np.URI); ok {
			np.URL = &u
		}
	}

	if np.Device != nil {
		d := *np.Device
		np.Device = &d
	}
	np.FetchedAt = np.FetchedAt.UTC().Truncate(time.Second)
	return np
}

// ParseURI splits a Spotify URI of the form spotify:<kind>:<rest>. Tracks and
// local files give ItemTrack, episodes ItemEpisode, ads ItemAd, and any other
// kind ItemUnknown. id is everything after the kind.
func ParseURI(uri string) (itemType ItemType, id string, ok bool) {
	kind, id, ok := splitURI(uri)
	if !ok {
		return "", "", false
	}
	switch kind {
	case "track", "local":
		return ItemTrack, id, true
	case "episode":
		return ItemEpisode, id, true
	case "ad":
		return ItemAd, id, true
	default:
		return ItemUnknown, id, true
	}
}

// OpenURL returns the open.spotify.com URL for a track or episode URI. It
// reports false for ads, local files, unknown kinds, and malformed ids.
func OpenURL(uri string) (string, bool) {
	kind, id, ok := splitURI(uri)
	if !ok || (kind != "track" && kind != "episode") || !isBase62(id) {
		return "", false
	}
	return urlBase + kind + "/" + id, true
}

func splitURI(uri string) (kind, rest string, ok bool) {
	body, found := strings.CutPrefix(uri, uriPrefix)
	if !found {
		return "", "", false
	}
	kind, rest, found = strings.Cut(body, ":")
	if !found || kind == "" || rest == "" {
		return "", "", false
	}
	return kind, rest, true
}

func isBase62(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		default:
			return false
		}
	}
	return true
}

func nonEmpty(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	v := *s
	return &v
}

func compact(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func positive[T int | int64](p *T) *T {
	if p == nil || *p <= 0 {
		return nil
	}
	v := *p
	return &v
}

func nonNegative[T int | int64](p *T) *T {
	if p == nil || *p < 0 {
		return nil
	}
	v := *p
	return &v
}
