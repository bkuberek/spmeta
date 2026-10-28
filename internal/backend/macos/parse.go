package macos

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

const (
	fieldSep   = "\x1f"
	fieldCount = 13
	uriPrefix  = "spotify:"
	markerOK   = "OK"
	notRunning = "NOT_RUNNING"
	stopped    = "STOPPED"
	noTrack    = "NO_TRACK"
)

// Indexes of the fields in an OK record.
const (
	fState = iota + 1
	fName
	fArtist
	fAlbum
	fAlbumArtist
	fDuration
	fPosition
	fID
	fSpotifyURL
	fArtwork
	fTrackNumber
	fDiscNumber
)

// parseOutput turns osascript stdout into a raw NowPlaying. The caller sets
// FetchedAt and normalizes.
func parseOutput(stdout []byte) (model.NowPlaying, error) {
	out := strings.TrimSuffix(string(stdout), "\n")
	switch out {
	case notRunning:
		return model.NowPlaying{}, errNotRunning()
	case stopped, noTrack:
		return model.NowPlaying{}, errNothingPlaying()
	}

	parts := strings.Split(out, fieldSep)
	if len(parts) != fieldCount || parts[0] != markerOK {
		return model.NowPlaying{}, errUnexpectedOutput(out)
	}
	state := model.State(parts[fState])
	if state != model.StatePlaying && state != model.StatePaused {
		return model.NowPlaying{}, errUnexpectedOutput(out)
	}

	np := model.NowPlaying{
		Source:      model.SourceMacOS,
		State:       state,
		Title:       text(parts[fName]),
		Album:       text(parts[fAlbum]),
		AlbumArtist: text(parts[fAlbumArtist]),
		DurationMS:  integer[int64](parts[fDuration]),
		PositionMS:  integer[int64](parts[fPosition]),
		URI:         uri(parts[fID], parts[fSpotifyURL]),
		ArtworkURL:  text(parts[fArtwork]),
		TrackNumber: integer[int](parts[fTrackNumber]),
		DiscNumber:  integer[int](parts[fDiscNumber]),
	}
	if artist := parts[fArtist]; artist != "" {
		np.Artists = []string{artist}
	}
	if np.URI == nil && np.Title == nil {
		return model.NowPlaying{}, errNothingPlaying()
	}
	return np, nil
}

func text(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func uri(id, spotifyURL string) *string {
	for _, c := range []string{id, spotifyURL} {
		if strings.HasPrefix(c, uriPrefix) {
			return &c
		}
	}
	return nil
}

// integer parses a base-10 integer; anything else, including an empty string,
// a decimal comma, or exponent notation, is nil.
func integer[T int | int64](s string) *T {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || int64(T(n)) != n {
		return nil
	}
	v := T(n)
	return &v
}

func errNotRunning() *backend.Error {
	return &backend.Error{Kind: backend.KindNotAvailable, Source: model.SourceMacOS, Message: msgNotRunning}
}

func errNothingPlaying() *backend.Error {
	return &backend.Error{Kind: backend.KindNothingPlaying, Source: model.SourceMacOS, Message: "nothing is playing"}
}

func errUnexpectedOutput(raw string) *backend.Error {
	return &backend.Error{
		Kind:    backend.KindUnexpected,
		Source:  model.SourceMacOS,
		Message: "unexpected output from osascript",
		Err:     fmt.Errorf("osascript output %q", raw),
	}
}
