package format

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bkuberek/spmeta/internal/model"
)

// ParseError describes an invalid template. Pos is the 1-based character
// position of the offending brace.
type ParseError struct {
	Pos int
	Msg string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s at position %d", e.Msg, e.Pos)
}

// fields maps each template field name to its value; ok is false for null.
var fields = map[string]func(*model.NowPlaying) (string, bool){
	"schema_version": func(np *model.NowPlaying) (string, bool) { return strconv.Itoa(np.SchemaVersion), true },
	"source":         func(np *model.NowPlaying) (string, bool) { return string(np.Source), true },
	"state":          func(np *model.NowPlaying) (string, bool) { return string(np.State), true },
	"type":           func(np *model.NowPlaying) (string, bool) { return string(np.Type), true },
	"state_icon":     func(np *model.NowPlaying) (string, bool) { return StateIcon(np.State), true },
	"title":          func(np *model.NowPlaying) (string, bool) { return str(np.Title) },
	"artist":         func(np *model.NowPlaying) (string, bool) { return str(np.Artist) },
	"artists":        func(np *model.NowPlaying) (string, bool) { return str(np.Artist) },
	"album":          func(np *model.NowPlaying) (string, bool) { return str(np.Album) },
	"album_artist":   func(np *model.NowPlaying) (string, bool) { return str(np.AlbumArtist) },
	"track_number":   func(np *model.NowPlaying) (string, bool) { return num(np.TrackNumber) },
	"disc_number":    func(np *model.NowPlaying) (string, bool) { return num(np.DiscNumber) },
	"duration_ms":    func(np *model.NowPlaying) (string, bool) { return num(np.DurationMS) },
	"position_ms":    func(np *model.NowPlaying) (string, bool) { return num(np.PositionMS) },
	"duration":       func(np *model.NowPlaying) (string, bool) { return clock(np.DurationMS) },
	"position":       func(np *model.NowPlaying) (string, bool) { return clock(np.PositionMS) },
	"uri":            func(np *model.NowPlaying) (string, bool) { return str(np.URI) },
	"url":            func(np *model.NowPlaying) (string, bool) { return str(np.URL) },
	"artwork_url":    func(np *model.NowPlaying) (string, bool) { return str(np.ArtworkURL) },
	"isrc":           func(np *model.NowPlaying) (string, bool) { return str(np.ISRC) },
	"explicit": func(np *model.NowPlaying) (string, bool) {
		if np.Explicit == nil {
			return "", false
		}
		return strconv.FormatBool(*np.Explicit), true
	},
	"device": func(np *model.NowPlaying) (string, bool) {
		if np.Device == nil {
			return "", false
		}
		return np.Device.Name, true
	},
	"device_type": func(np *model.NowPlaying) (string, bool) {
		if np.Device == nil {
			return "", false
		}
		return np.Device.Type, true
	},
	"fetched_at": func(np *model.NowPlaying) (string, bool) {
		return np.FetchedAt.UTC().Format(time.RFC3339), true
	},
}

func str(s *string) (string, bool) {
	if s == nil {
		return "", false
	}
	return *s, true
}

func num[T int | int64](n *T) (string, bool) {
	if n == nil {
		return "", false
	}
	return strconv.FormatInt(int64(*n), 10), true
}

func clock(ms *int64) (string, bool) {
	if ms == nil {
		return "", false
	}
	return model.FormatDuration(*ms), true
}

// Template is a parsed {field} template.
type Template struct {
	parts []part
}

// part is literal text or, when field is set, a field reference.
type part struct {
	text  string
	field func(*model.NowPlaying) (string, bool)
}

// ParseTemplate parses the {field} mini-syntax: {{ and }} are literal braces,
// and an unknown field, an unclosed { or a stray } is a *ParseError.
func ParseTemplate(s string) (*Template, error) {
	var (
		t   Template
		lit strings.Builder
	)
	flush := func() {
		if lit.Len() > 0 {
			t.parts = append(t.parts, part{text: lit.String()})
			lit.Reset()
		}
	}
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		switch r := runes[i]; r {
		case '{':
			if i+1 < len(runes) && runes[i+1] == '{' {
				lit.WriteRune('{')
				i++
				continue
			}
			end := closingBrace(runes, i+1)
			if end < 0 {
				return nil, &ParseError{Pos: i + 1, Msg: "unclosed '{'"}
			}
			name := string(runes[i+1 : end])
			if name == "" {
				return nil, &ParseError{Pos: i + 1, Msg: "empty field name"}
			}
			get, ok := fields[name]
			if !ok {
				return nil, &ParseError{Pos: i + 1, Msg: fmt.Sprintf("unknown field %q", name)}
			}
			flush()
			t.parts = append(t.parts, part{field: get})
			i = end
		case '}':
			if i+1 < len(runes) && runes[i+1] == '}' {
				lit.WriteRune('}')
				i++
				continue
			}
			return nil, &ParseError{Pos: i + 1, Msg: "unmatched '}'"}
		default:
			lit.WriteRune(r)
		}
	}
	flush()
	return &t, nil
}

// closingBrace returns the index of the first '}' at or after from, or -1 when
// there is none or another '{' comes first.
func closingBrace(runes []rune, from int) int {
	for i := from; i < len(runes); i++ {
		switch runes[i] {
		case '}':
			return i
		case '{':
			return -1
		}
	}
	return -1
}

// Render fills the template from np without a trailing newline. Null fields
// render empty, field values are sanitized to one line, and a nil np (nothing
// playing) renders as the empty string.
func (t *Template) Render(np *model.NowPlaying) string {
	if np == nil {
		return ""
	}
	var b strings.Builder
	for _, p := range t.parts {
		if p.field == nil {
			b.WriteString(p.text)
			continue
		}
		if v, ok := p.field(np); ok {
			b.WriteString(sanitize(v))
		}
	}
	return b.String()
}
