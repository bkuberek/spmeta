package mpris

import (
	"reflect"
	"testing"
)

// variantLike has the shape of dbus.Variant: a Value method returning the
// wrapped value.
type variantLike struct{ v any }

func (v variantLike) Value() any { return v.v }

// pathLike has the kind of dbus.ObjectPath.
type pathLike string

func TestUnwrap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   any
		want any
	}{
		{"nil", nil, nil},
		{"string", "Playing", "Playing"},
		{"int64", int64(120165000), int64(120165000)},
		{"int32", int32(4), int32(4)},
		{"uint32", uint32(4), uint32(4)},
		{"float64", 1.5, 1.5},
		{"bool", true, true},
		{"string slice", []string{"Alvan", "Other"}, []string{"Alvan", "Other"}},
		{"bytes stay bytes", []byte{1, 2}, []byte{1, 2}},
		{"object path becomes string", pathLike("/com/spotify/track/abc"), "/com/spotify/track/abc"},
		{"variant", variantLike{"Paused"}, "Paused"},
		{"nested variant", variantLike{variantLike{int64(7)}}, int64(7)},
		{"variant of object path", variantLike{pathLike("/spotify/track/abc")}, "/spotify/track/abc"},
		{"variant of string slice", variantLike{[]string{"A"}}, []string{"A"}},
		{
			"metadata map",
			map[string]variantLike{
				"mpris:trackid": {pathLike("/com/spotify/track/abc")},
				"xesam:title":   {"Amazone"},
				"xesam:artist":  {[]string{"Alvan"}},
				"mpris:length":  {int64(238341000)},
			},
			map[string]any{
				"mpris:trackid": "/com/spotify/track/abc",
				"xesam:title":   "Amazone",
				"xesam:artist":  []string{"Alvan"},
				"mpris:length":  int64(238341000),
			},
		},
		{
			"map inside a variant inside a map",
			map[string]variantLike{"Metadata": {map[string]variantLike{"xesam:album": {"Home"}}}},
			map[string]any{"Metadata": map[string]any{"xesam:album": "Home"}},
		},
		{"object path slice becomes string slice", []pathLike{"/a", "/b"}, []string{"/a", "/b"}},
		{"variant slice becomes any slice", []variantLike{{"a"}, {int64(1)}}, []any{"a", int64(1)}},
		{"int slice becomes any slice", []int32{1, 2}, []any{int32(1), int32(2)}},
		{"empty string slice", []string{}, []string{}},
		{"map with non-string keys is untouched", map[int]string{1: "a"}, map[int]string{1: "a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := unwrap(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("unwrap(%#v) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestUnwrapProperties(t *testing.T) {
	t.Parallel()
	got := unwrapProperties(map[string]variantLike{
		"PlaybackStatus": {"Playing"},
		"Position":       {int64(5)},
		"Metadata":       {map[string]variantLike{"mpris:trackid": {pathLike("/x")}}},
	})
	want := Properties{
		"PlaybackStatus": "Playing",
		"Position":       int64(5),
		"Metadata":       map[string]any{"mpris:trackid": "/x"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unwrapProperties = %#v, want %#v", got, want)
	}
}
