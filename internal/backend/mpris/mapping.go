package mpris

import (
	"fmt"
	"math"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

const (
	noTrackPath    = "/org/mpris/MediaPlayer2/TrackList/NoTrack"
	openURLBase    = "https://open.spotify.com/"
	artworkHost    = "open.spotify.com"
	artworkCDN     = "https://i.scdn.co/image/"
	localURIPrefix = "spotify:local:"
	microPerMill   = 1000
)

var (
	trackIDPattern = regexp.MustCompile(`(track|episode|ad)[/:]([A-Za-z0-9]+)$`)
	openURLPattern = regexp.MustCompile(`^https://open\.spotify\.com/(track|episode)/([A-Za-z0-9]+)(?:[?#].*)?$`)
)

// mapProperties converts a player's properties into a raw NowPlaying. player
// is the bus name suffix, used in error messages. It returns a nothing-playing
// error when no item is loaded. PositionMS is the reported Position; the
// caller applies the Spotify desktop rule.
func mapProperties(player string, p Properties) (model.NowPlaying, error) {
	state, err := parseState(player, p)
	if err != nil {
		return model.NowPlaying{}, err
	}
	meta, _ := p["Metadata"].(map[string]any)
	trackID, _ := meta["mpris:trackid"].(string)
	if len(meta) == 0 || trackID == noTrackPath {
		return model.NowPlaying{}, errNothingPlaying()
	}

	rawURL, _ := meta["xesam:url"].(string)
	np := model.NowPlaying{
		Source:      model.SourceMPRIS,
		State:       state,
		Title:       optString(meta["xesam:title"]),
		Artists:     asStrings(meta["xesam:artist"]),
		Album:       optString(meta["xesam:album"]),
		AlbumArtist: optString(strings.Join(asStrings(meta["xesam:albumArtist"]), ", ")),
		TrackNumber: optInt(meta["xesam:trackNumber"]),
		DiscNumber:  optInt(meta["xesam:discNumber"]),
		DurationMS:  optMillis(meta["mpris:length"]),
		PositionMS:  optMillis(p["Position"]),
		URI:         trackURI(trackID, rawURL),
		ArtworkURL:  artworkURL(meta["mpris:artUrl"]),
	}
	if strings.HasPrefix(rawURL, openURLBase) {
		np.URL = &rawURL
	}
	if isLocalFile(trackID, rawURL) {
		// A local file is a track even when its object-path trackid cannot be
		// turned back into a spotify:local: URI (data-model.md, Local files).
		np.Type = model.ItemTrack
	}
	if state == model.StateStopped && np.URI == nil && np.Title == nil {
		return model.NowPlaying{}, errNothingPlaying()
	}
	return np, nil
}

func parseState(player string, p Properties) (model.State, error) {
	status, _ := p["PlaybackStatus"].(string)
	switch status {
	case "Playing":
		return model.StatePlaying, nil
	case "Paused":
		return model.StatePaused, nil
	case "Stopped":
		return model.StateStopped, nil
	}
	return "", &backend.Error{
		Kind:    backend.KindUnexpected,
		Source:  model.SourceMPRIS,
		Message: fmt.Sprintf("unexpected PlaybackStatus %q from %s", status, player),
	}
}

func errNothingPlaying() *backend.Error {
	return &backend.Error{Kind: backend.KindNothingPlaying, Source: model.SourceMPRIS, Message: "nothing is playing"}
}

func isLocalFile(trackID, rawURL string) bool {
	return strings.HasPrefix(trackID, localURIPrefix) ||
		strings.Contains(trackID, "/local/") ||
		strings.HasPrefix(rawURL, "file://")
}

// trackURI prefers mpris:trackid, then an open.spotify.com URL, then a
// spotify: URI carried in xesam:url. A spotify:local: trackid is kept as is.
func trackURI(trackID, rawURL string) *string {
	if strings.HasPrefix(trackID, localURIPrefix) {
		return &trackID
	}
	for _, re := range []struct {
		pattern *regexp.Regexp
		input   string
	}{{trackIDPattern, trackID}, {openURLPattern, rawURL}} {
		if m := re.pattern.FindStringSubmatch(re.input); m != nil {
			uri := "spotify:" + m[1] + ":" + m[2]
			return &uri
		}
	}
	if strings.HasPrefix(rawURL, "spotify:") {
		return &rawURL
	}
	return nil
}

// artworkURL accepts HTTPS only and rewrites Spotify desktop's
// open.spotify.com/image links to the CDN host that serves the same image.
func artworkURL(v any) *string {
	raw, _ := v.(string)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return nil
	}
	if u.Host == artworkHost && strings.HasPrefix(u.Path, "/image/") {
		rewritten := artworkCDN + path.Base(u.Path)
		return &rewritten
	}
	return &raw
}

func optString(v any) *string {
	s, _ := v.(string)
	if s == "" {
		return nil
	}
	return &s
}

func optInt(v any) *int {
	n, ok := asInt64(v)
	if !ok {
		return nil
	}
	i := int(n)
	return &i
}

// optMillis converts a microsecond value to milliseconds.
func optMillis(v any) *int64 {
	n, ok := asInt64(v)
	if !ok {
		return nil
	}
	n /= microPerMill
	return &n
}

// asStrings accepts []string, a single string, or []any holding strings.
func asStrings(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// asInt64 accepts every integer type and float64 (truncated toward zero). It
// reports false for other types and for values outside the int64 range.
func asInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case int:
		return int64(t), true
	case int8:
		return int64(t), true
	case int16:
		return int64(t), true
	case int32:
		return int64(t), true
	case int64:
		return t, true
	case uint:
		return fromUint(uint64(t))
	case uint8:
		return int64(t), true
	case uint16:
		return int64(t), true
	case uint32:
		return int64(t), true
	case uint64:
		return fromUint(t)
	case float32:
		return fromFloat(float64(t))
	case float64:
		return fromFloat(t)
	}
	return 0, false
}

func fromUint(u uint64) (int64, bool) {
	if u > math.MaxInt64 {
		return 0, false
	}
	return int64(u), true
}

func fromFloat(f float64) (int64, bool) {
	f = math.Trunc(f)
	if math.IsNaN(f) || f < math.MinInt64 || f >= math.MaxInt64 {
		return 0, false
	}
	return int64(f), true
}
