package format_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/bkuberek/spmeta/internal/format"
	"github.com/bkuberek/spmeta/schema"
)

const schemaID = "https://raw.githubusercontent.com/bkuberek/spmeta/main/schema/spmeta.schema.json"

func compileSchema(t *testing.T) *jsonschema.Schema {
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

func TestJSONGolden(t *testing.T) {
	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			got, err := format.JSON(f.np)
			if err != nil {
				t.Fatal(err)
			}
			checkGolden(t, "json/"+f.name+".json", got)
		})
	}
	t.Run("nothing", func(t *testing.T) {
		got, err := format.JSON(nil)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "null\n" {
			t.Errorf("JSON(nil) = %q, want %q", got, "null\n")
		}
	})
}

func TestJSONOneLine(t *testing.T) {
	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			got, err := format.JSON(f.np)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasSuffix(got, []byte("\n")) || bytes.Count(got, []byte("\n")) != 1 {
				t.Errorf("want exactly one trailing newline, got %q", got)
			}
		})
	}
}

func TestJSONKeepsValuesUnchanged(t *testing.T) {
	got, err := format.JSON(byName(t, "control_chars"))
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatal(err)
	}
	if back["title"] != "Line1\nLine2\tTab" {
		t.Errorf("title = %q, want newline and tab preserved", back["title"])
	}
}

func TestJSONDoesNotEscapeHTML(t *testing.T) {
	np := base()
	np.Title = ptr("Tom & <Jerry>")
	got, err := format.JSON(&np)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"Tom & <Jerry>"`) {
		t.Errorf("got %s, want unescaped title", got)
	}
}

func TestJSONMatchesDataModelExample(t *testing.T) {
	const want = `{"schema_version":1,"source":"macos","state":"playing","type":"track","title":"Amazone","artists":["Alvan"],"artist":"Alvan","album":"Home","album_artist":"Alvan","track_number":4,"disc_number":1,"duration_ms":238341,"position_ms":120165,"uri":"spotify:track:4brK3gpPjXArNUlU1hBXTe","url":"https://open.spotify.com/track/4brK3gpPjXArNUlU1hBXTe","artwork_url":"https://i.scdn.co/image/ab67616d0000b273a9ab54d90005cfd439f821e8","isrc":null,"explicit":null,"device":null,"fetched_at":"2026-10-27T17:40:00Z"}` + "\n"
	np := base()
	got, err := format.JSON(&np)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestGoldenJSONValidatesAgainstSchema(t *testing.T) {
	s := compileSchema(t)
	files, err := filepath.Glob(filepath.Join("testdata", "json", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < len(fixtures()) {
		t.Fatalf("found %d golden JSON files, want at least %d", len(files), len(fixtures()))
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if err := s.Validate(inst); err != nil {
				t.Errorf("schema validation: %v", err)
			}
		})
	}
}
