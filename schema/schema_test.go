package schema_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/bkuberek/spmeta/internal/model"
	"github.com/bkuberek/spmeta/schema"
)

const schemaID = "https://raw.githubusercontent.com/bkuberek/spmeta/main/schema/spmeta.schema.json"

const example = `{"schema_version":1,"source":"macos","state":"playing","type":"track","title":"Amazone","artists":["Alvan"],"artist":"Alvan","album":"Home","album_artist":"Alvan","track_number":4,"disc_number":1,"duration_ms":238341,"position_ms":120165,"uri":"spotify:track:4brK3gpPjXArNUlU1hBXTe","url":"https://open.spotify.com/track/4brK3gpPjXArNUlU1hBXTe","artwork_url":"https://i.scdn.co/image/ab67616d0000b273a9ab54d90005cfd439f821e8","isrc":null,"explicit":null,"device":null,"fetched_at":"2026-10-27T17:40:00Z"}`

func compile(t *testing.T) *jsonschema.Schema {
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

func validate(t *testing.T, s *jsonschema.Schema, doc string) error {
	t.Helper()
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("parse instance: %v", err)
	}
	return s.Validate(inst)
}

func mutate(t *testing.T, fn func(m map[string]any)) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(example), &m); err != nil {
		t.Fatal(err)
	}
	fn(m)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAccepts(t *testing.T) {
	t.Parallel()
	s := compile(t)
	tests := []struct {
		name string
		doc  string
	}{
		{"data-model example", example},
		{"web with device", mutate(t, func(m map[string]any) {
			m["source"] = "web"
			m["isrc"] = "USRC17607839"
			m["explicit"] = false
			m["device"] = map[string]any{"name": "Mac", "type": "Computer", "volume_percent": nil}
		})},
		{"mpris stopped with nulls", mutate(t, func(m map[string]any) {
			m["source"] = "mpris"
			m["state"] = "stopped"
			m["position_ms"] = nil
			m["artists"] = []any{}
			m["artist"] = nil
		})},
		{"ad with nulls", mutate(t, func(m map[string]any) {
			m["type"] = "ad"
			m["title"] = nil
			m["url"] = nil
			m["uri"] = "spotify:ad:abc"
		})},
		{"unknown extra field", mutate(t, func(m map[string]any) { m["future"] = true })},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := validate(t, s, tc.doc); err != nil {
				t.Errorf("want valid, got: %v", err)
			}
		})
	}
}

func TestRejects(t *testing.T) {
	t.Parallel()
	s := compile(t)
	tests := []struct {
		name string
		doc  string
	}{
		{"empty title", mutate(t, func(m map[string]any) { m["title"] = "" })},
		{"missing key", mutate(t, func(m map[string]any) { delete(m, "isrc") })},
		{"device on macos", mutate(t, func(m map[string]any) {
			m["device"] = map[string]any{"name": "Mac", "type": "Computer", "volume_percent": 10}
		})},
		{"isrc on mpris", mutate(t, func(m map[string]any) { m["source"] = "mpris"; m["isrc"] = "X" })},
		{"stopped on web", mutate(t, func(m map[string]any) { m["source"] = "web"; m["state"] = "stopped" })},
		{"null artists", mutate(t, func(m map[string]any) { m["artists"] = nil })},
		{"empty artist entry", mutate(t, func(m map[string]any) { m["artists"] = []any{""} })},
		{"zero track number", mutate(t, func(m map[string]any) { m["track_number"] = 0 })},
		{"wrong schema version", mutate(t, func(m map[string]any) { m["schema_version"] = 2 })},
		{"non-utc fetched_at", mutate(t, func(m map[string]any) { m["fetched_at"] = "2026-10-27T19:40:00+02:00" })},
		{"unknown source", mutate(t, func(m map[string]any) { m["source"] = "tidal" })},
		{"literal null", "null"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := validate(t, s, tc.doc); err == nil {
				t.Errorf("want invalid, got valid: %s", tc.doc)
			}
		})
	}
}

func TestNormalizedModelValidates(t *testing.T) {
	t.Parallel()
	s := compile(t)
	title := "Amazone"
	uri := "spotify:track:4brK3gpPjXArNUlU1hBXTe"
	tests := map[string]model.NowPlaying{
		"empty": {Source: model.SourceMPRIS, State: model.StatePaused},
		"track": {
			Source: model.SourceMacOS, State: model.StatePlaying, Title: &title, URI: &uri,
			Artists: []string{"Alvan"}, FetchedAt: time.Now(),
		},
	}
	for name, np := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b, err := json.Marshal(model.Normalize(np))
			if err != nil {
				t.Fatal(err)
			}
			if err := validate(t, s, string(b)); err != nil {
				t.Errorf("%s: %v", b, err)
			}
		})
	}
}
