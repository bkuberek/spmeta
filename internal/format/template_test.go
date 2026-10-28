package format_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/bkuberek/spmeta/internal/format"
	"github.com/bkuberek/spmeta/internal/model"
)

const allFields = "schema_version={schema_version} source={source} state={state} icon={state_icon} type={type}" +
	" title={title} artist={artist} artists={artists} album={album} album_artist={album_artist}" +
	" track={track_number} disc={disc_number} duration_ms={duration_ms} position_ms={position_ms}" +
	" duration={duration} position={position} uri={uri} url={url} art={artwork_url} isrc={isrc}" +
	" explicit={explicit} device={device} device_type={device_type} fetched_at={fetched_at}"

func render(t *testing.T, tmpl string, np *model.NowPlaying) string {
	t.Helper()
	tpl, err := format.ParseTemplate(tmpl)
	if err != nil {
		t.Fatalf("ParseTemplate(%q): %v", tmpl, err)
	}
	return tpl.Render(np)
}

func TestTemplateGolden(t *testing.T) {
	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			checkGolden(t, "template/"+f.name+".golden", []byte(render(t, allFields, f.np)))
		})
	}
}

func TestTemplateRender(t *testing.T) {
	full := byName(t, "playing_full")
	web := byName(t, "web_paused_device")
	hour := byName(t, "episode_hour")
	allNull := byName(t, "all_null")
	tests := []struct {
		name string
		tmpl string
		np   *model.NowPlaying
		want string
	}{
		{"artist and title", "{artist} - {title}", full, "Alvan - Amazone"},
		{"literal only", "no fields", full, "no fields"},
		{"empty template", "", full, ""},
		{"escaped braces", "{{title}} = {title}", full, "{title} = Amazone"},
		{"escaped close", "a}}b", full, "a}b"},
		{"triple open", "{{{title}", full, "{Amazone"},
		{"nulls render empty", "[{album}|{isrc}|{explicit}|{device}|{position}]", allNull, "[||||]"},
		{"state icon", "{state_icon}", web, "⏸"},
		{"durations", "{position} / {duration}", full, "2:00 / 3:58"},
		{"hour duration", "{position} / {duration}", hour, "1:00:00 / 1:02:05"},
		{"integers decimal", "{track_number}/{disc_number} {duration_ms} {position_ms}", full, "4/1 238341 120165"},
		{"booleans", "{explicit}", web, "false"},
		{"device fields", "{device} ({device_type})", web, "Kitchen (Speaker)"},
		{"fetched_at", "{fetched_at}", full, "2026-10-27T17:40:00Z"},
		{"artists equals artist", "{artists}", full, "Alvan"},
		{"nothing playing", "{artist} - {title}", nil, ""},
		{"control chars in values", "{title}|{artist}|{album}", byName(t, "control_chars"), "Line1 Line2 Tab|A  B|Album"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := render(t, tc.tmpl, tc.np); got != tc.want {
				t.Errorf("Render(%q) = %q, want %q", tc.tmpl, got, tc.want)
			}
		})
	}
}

func TestParseTemplateErrors(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    string
		pos     int
		contain string
	}{
		{"unknown field", "{nope}", 1, `unknown field "nope"`},
		{"unknown field later", "a {title} {Title}", 11, `unknown field "Title"`},
		{"empty field", "x{}", 2, "empty field"},
		{"unclosed", "a {title", 3, "unclosed"},
		{"unclosed at end", "{", 1, "unclosed"},
		{"stray close", "a}b", 2, "unmatched"},
		{"stray after escape", "{{x}", 4, "unmatched"},
		{"nested open", "{a{b}", 1, "unclosed"},
		{"multibyte position", "é {nope}", 3, "unknown field"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := format.ParseTemplate(tc.tmpl)
			var pe *format.ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want *ParseError", err)
			}
			if pe.Pos != tc.pos {
				t.Errorf("Pos = %d, want %d", pe.Pos, tc.pos)
			}
			if !strings.Contains(pe.Error(), tc.contain) || !strings.Contains(pe.Error(), "position") {
				t.Errorf("Error() = %q, want it to contain %q and the position", pe.Error(), tc.contain)
			}
		})
	}
}

func TestParseTemplateAcceptsEveryDocumentedField(t *testing.T) {
	if _, err := format.ParseTemplate(allFields); err != nil {
		t.Fatalf("allFields: %v", err)
	}
}
