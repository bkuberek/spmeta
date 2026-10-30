package format_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bkuberek/spmeta/internal/format"
	"github.com/bkuberek/spmeta/internal/model"
)

// waybarOut is the decoded form of one Waybar line. Percentage is a pointer so
// that an omitted key is distinguishable from 0.
type waybarOut struct {
	Text       string `json:"text"`
	Alt        string `json:"alt"`
	Tooltip    string `json:"tooltip"`
	Class      string `json:"class"`
	Percentage *int   `json:"percentage"`
}

func renderWaybar(t *testing.T, np *model.NowPlaying) (raw string, out waybarOut) {
	t.Helper()
	got, err := format.Waybar(np)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("not valid JSON: %v\n%q", err, got)
	}
	return string(got), out
}

func TestWaybarGolden(t *testing.T) {
	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			got, err := format.Waybar(f.np)
			if err != nil {
				t.Fatal(err)
			}
			checkGolden(t, "waybar/"+f.name+".json", got)
		})
	}
}

func TestWaybarExactLines(t *testing.T) {
	tests := []struct {
		name string
		np   *model.NowPlaying
		want string
	}{
		{
			"playing",
			byName(t, "playing_full"),
			`{"text":"▶ Amazone — Alvan · Home (2:00 / 3:58)","alt":"playing","tooltip":"Amazone\nAlvan\nHome\nSource: macos","class":"playing","percentage":50}` + "\n",
		},
		{
			"nothing playing",
			nil,
			`{"text":"","alt":"none","class":"none"}` + "\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := format.Waybar(tc.np)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("Waybar() =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestWaybarOneCompactLine(t *testing.T) {
	for _, f := range append(fixtures(), fixture{"nothing", nil}) {
		t.Run(f.name, func(t *testing.T) {
			raw, _ := renderWaybar(t, f.np)
			if !strings.HasSuffix(raw, "}\n") || strings.Count(raw, "\n") != 1 {
				t.Errorf("want exactly one newline-terminated line, got %q", raw)
			}
			if strings.Contains(raw, `&`) || strings.Contains(raw, `<`) {
				t.Errorf("markup escapes must stay literal, got %q", raw)
			}
		})
	}
}

func TestWaybarTextMatchesTextFormat(t *testing.T) {
	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			_, out := renderWaybar(t, f.np)
			if want := format.Text(f.np); out.Text != want {
				t.Errorf("text = %q, want %q", out.Text, want)
			}
		})
	}
}

func TestWaybarStateMapsToClassAndAlt(t *testing.T) {
	tests := []struct {
		name  string
		state model.State
		want  string
	}{
		{"playing", model.StatePlaying, "playing"},
		{"paused", model.StatePaused, "paused"},
		{"stopped", model.StateStopped, "stopped"},
		{"unknown state is stopped", model.State("bogus"), "stopped"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			np := *byName(t, "playing_full")
			np.State = tc.state
			_, out := renderWaybar(t, &np)
			if out.Class != tc.want || out.Alt != tc.want {
				t.Errorf("class = %q, alt = %q; want both %q", out.Class, out.Alt, tc.want)
			}
		})
	}
	t.Run("nothing playing", func(t *testing.T) {
		_, out := renderWaybar(t, nil)
		if out.Class != "none" || out.Alt != "none" || out.Text != "" || out.Tooltip != "" {
			t.Errorf("got %+v, want empty text and none class/alt", out)
		}
	})
}

func TestWaybarPercentage(t *testing.T) {
	tests := []struct {
		name     string
		position *int64
		duration *int64
		want     *int
	}{
		{"half", ptr(int64(1000)), ptr(int64(2000)), ptr(50)},
		{"start", ptr(int64(0)), ptr(int64(2000)), ptr(0)},
		{"truncates", ptr(int64(1999)), ptr(int64(2000)), ptr(99)},
		{"end", ptr(int64(2000)), ptr(int64(2000)), ptr(100)},
		{"position past duration clamps", ptr(int64(5000)), ptr(int64(2000)), ptr(100)},
		{"negative position clamps", ptr(int64(-5)), ptr(int64(2000)), ptr(0)},
		{"no position", nil, ptr(int64(2000)), nil},
		{"no duration", ptr(int64(1000)), nil, nil},
		{"zero duration", ptr(int64(1000)), ptr(int64(0)), nil},
		{"neither", nil, nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			np := *byName(t, "playing_full")
			np.PositionMS, np.DurationMS = tc.position, tc.duration
			raw, out := renderWaybar(t, &np)
			switch {
			case tc.want == nil && strings.Contains(raw, "percentage"):
				t.Errorf("percentage must be omitted, got %q", raw)
			case tc.want != nil && (out.Percentage == nil || *out.Percentage != *tc.want):
				t.Errorf("percentage = %v, want %d (%q)", out.Percentage, *tc.want, raw)
			}
		})
	}
}

func TestWaybarEscapesPangoMarkup(t *testing.T) {
	np := *byName(t, "playing_full")
	np.Title = ptr("Rock & Roll <Live>")
	np.Artists = []string{"AC/DC & Co", "<b>Bold</b>"}
	np.Artist = ptr("AC/DC & Co, <b>Bold</b>")
	np.Album = ptr("Q&A > Answers")
	raw, out := renderWaybar(t, &np)

	for _, field := range []struct{ name, got, want string }{
		{"text", out.Text, "▶ Rock &amp; Roll &lt;Live&gt; — AC/DC &amp; Co, &lt;b&gt;Bold&lt;/b&gt; · Q&amp;A &gt; Answers (2:00 / 3:58)"},
		{"tooltip", out.Tooltip, "Rock &amp; Roll &lt;Live&gt;\nAC/DC &amp; Co, &lt;b&gt;Bold&lt;/b&gt;\nQ&amp;A &gt; Answers\nSource: macos"},
	} {
		if field.got != field.want {
			t.Errorf("%s = %q, want %q", field.name, field.got, field.want)
		}
	}
	if strings.Contains(raw, "<") || strings.Contains(raw, ">") {
		t.Errorf("raw output contains an unescaped angle bracket: %q", raw)
	}
}

func TestWaybarTooltip(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    string
	}{
		{"all parts", "playing_full", "Amazone\nAlvan\nHome\nSource: macos"},
		{"no album", "no_album", "Amazone\nAlvan\nSource: macos"},
		{"no artist", "no_artist", "Amazone\nHome\nSource: macos"},
		{"several artists are joined", "web_paused_device", "Blinding Lights\nThe Weeknd\nAfter Hours\nSource: web"},
		{"no title falls back like text", "no_title", "(no title)\nAlvan\nHome\nSource: macos"},
		{"ad without title", "ad_no_title", "Advertisement\nSource: macos"},
		{"control characters stay inside their line", "control_chars", "Line1 Line2 Tab\nA  B\nAlbum\nSource: macos"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, out := renderWaybar(t, byName(t, tc.fixture))
			if out.Tooltip != tc.want {
				t.Errorf("tooltip = %q, want %q", out.Tooltip, tc.want)
			}
		})
	}

	t.Run("artists list wins over the joined artist", func(t *testing.T) {
		np := *byName(t, "playing_full")
		np.Artists = []string{"One", "Two"}
		np.Artist = ptr("One, Two")
		_, out := renderWaybar(t, &np)
		if want := "Amazone\nOne, Two\nHome\nSource: macos"; out.Tooltip != want {
			t.Errorf("tooltip = %q, want %q", out.Tooltip, want)
		}
	})
}
