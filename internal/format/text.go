// Package format renders a NowPlaying value as text, JSON or a user template.
// See docs/design/cli.md, Output formats.
package format

import (
	"strings"
	"unicode"

	"github.com/bkuberek/spmeta/internal/model"
)

const (
	iconPlaying = "▶"
	iconPaused  = "⏸"
	iconStopped = "■"
)

// StateIcon returns the glyph for a playback state; unknown states render as
// stopped.
func StateIcon(s model.State) string {
	switch s {
	case model.StatePlaying:
		return iconPlaying
	case model.StatePaused:
		return iconPaused
	default:
		return iconStopped
	}
}

// Text renders the default one-line format without a trailing newline. A nil
// np (nothing playing) renders as the empty string.
func Text(np *model.NowPlaying) string {
	if np == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(StateIcon(np.State))
	b.WriteString(" ")
	b.WriteString(textTitle(np))
	if np.Artist != nil {
		b.WriteString(" — " + sanitize(*np.Artist))
	}
	if np.Album != nil {
		b.WriteString(" · " + sanitize(*np.Album))
	}
	if np.DurationMS != nil {
		b.WriteString(" (")
		if np.PositionMS != nil {
			b.WriteString(model.FormatDuration(*np.PositionMS) + " / ")
		}
		b.WriteString(model.FormatDuration(*np.DurationMS) + ")")
	}
	return b.String()
}

func textTitle(np *model.NowPlaying) string {
	switch {
	case np.Title != nil:
		return sanitize(*np.Title)
	case np.Type == model.ItemAd:
		return "Advertisement"
	default:
		return "(no title)"
	}
}

// sanitize keeps one item on one line: newline, carriage return and tab become
// one space each, and other control characters are dropped.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, s)
}
