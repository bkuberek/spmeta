package format

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/bkuberek/spmeta/internal/model"
)

const classNone = "none"

// waybarLine is one Waybar custom-module line (return-type json). Optional
// keys are omitted rather than written as null or empty.
type waybarLine struct {
	Text       string `json:"text"`
	Alt        string `json:"alt"`
	Tooltip    string `json:"tooltip,omitempty"`
	Class      string `json:"class"`
	Percentage *int   `json:"percentage,omitempty"`
}

// markupEscaper escapes the characters that Pango markup treats as syntax.
// Waybar parses text and tooltip as markup, and quotes are plain text there.
var markupEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// Waybar renders np as one compact JSON object for a Waybar custom module,
// followed by a newline. A nil np (nothing playing) renders as an empty text
// with class and alt "none", so the module clears. Text and tooltip are
// Pango-escaped, and the encoder keeps the resulting entities literal.
func Waybar(np *model.NowPlaying) ([]byte, error) {
	line := waybarLine{Alt: classNone, Class: classNone}
	if np != nil {
		state := stateName(np.State)
		line = waybarLine{
			Text:       markupEscaper.Replace(Text(np)),
			Alt:        state,
			Tooltip:    markupEscaper.Replace(waybarTooltip(np)),
			Class:      state,
			Percentage: progressPercent(np),
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(line); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// stateName returns the state as the class and alt value; unknown states are
// stopped, as in StateIcon.
func stateName(s model.State) string {
	switch s {
	case model.StatePlaying, model.StatePaused:
		return string(s)
	default:
		return string(model.StateStopped)
	}
}

// waybarTooltip lists the title, the artists, the album and the source, one
// per line, leaving out a part that has no value. The title always shows, with
// the same fallback as Text.
func waybarTooltip(np *model.NowPlaying) string {
	lines := []string{textTitle(np)}
	if artists := tooltipArtists(np); artists != "" {
		lines = append(lines, artists)
	}
	if np.Album != nil {
		lines = append(lines, sanitize(*np.Album))
	}
	lines = append(lines, "Source: "+string(np.Source))
	return strings.Join(lines, "\n")
}

func tooltipArtists(np *model.NowPlaying) string {
	switch {
	case len(np.Artists) > 0:
		return sanitize(strings.Join(np.Artists, ", "))
	case np.Artist != nil:
		return sanitize(*np.Artist)
	default:
		return ""
	}
}

// progressPercent is the position as a whole percent of the duration, rounded
// down and clamped to 0-100. It is nil unless both values are known.
func progressPercent(np *model.NowPlaying) *int {
	if np.PositionMS == nil || np.DurationMS == nil || *np.DurationMS <= 0 {
		return nil
	}
	pct := int(min(max(*np.PositionMS*100 / *np.DurationMS, 0), 100))
	return &pct
}
