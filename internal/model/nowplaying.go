// Package model defines the NowPlaying object every backend produces and the
// normalization applied to it. See docs/design/data-model.md.
package model

import (
	"encoding/json"
	"time"
)

// SchemaVersion is the value of the schema_version field.
const SchemaVersion = 1

// Source identifies the backend that produced a NowPlaying.
type Source string

// Known sources.
const (
	SourceMacOS Source = "macos"
	SourceMPRIS Source = "mpris"
	SourceWeb   Source = "web"
)

// State is the playback state.
type State string

// Known playback states.
const (
	StatePlaying State = "playing"
	StatePaused  State = "paused"
	StateStopped State = "stopped"
)

// ItemType is the kind of item that is loaded.
type ItemType string

// Known item types.
const (
	ItemTrack   ItemType = "track"
	ItemEpisode ItemType = "episode"
	ItemAd      ItemType = "ad"
	ItemUnknown ItemType = "unknown"
)

// Device is the playback device reported by the Web source.
type Device struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	VolumePercent *int   `json:"volume_percent"`
}

// NowPlaying is the public output object. Field order is the JSON key order.
type NowPlaying struct {
	SchemaVersion int       `json:"schema_version"`
	Source        Source    `json:"source"`
	State         State     `json:"state"`
	Type          ItemType  `json:"type"`
	Title         *string   `json:"title"`
	Artists       []string  `json:"artists"`
	Artist        *string   `json:"artist"`
	Album         *string   `json:"album"`
	AlbumArtist   *string   `json:"album_artist"`
	TrackNumber   *int      `json:"track_number"`
	DiscNumber    *int      `json:"disc_number"`
	DurationMS    *int64    `json:"duration_ms"`
	PositionMS    *int64    `json:"position_ms"`
	URI           *string   `json:"uri"`
	URL           *string   `json:"url"`
	ArtworkURL    *string   `json:"artwork_url"`
	ISRC          *string   `json:"isrc"`
	Explicit      *bool     `json:"explicit"`
	Device        *Device   `json:"device"`
	FetchedAt     time.Time `json:"fetched_at"`
}

// MarshalJSON marshals Artists as [] rather than null when it is empty.
func (np NowPlaying) MarshalJSON() ([]byte, error) {
	type plain NowPlaying
	if np.Artists == nil {
		np.Artists = []string{}
	}
	return json.Marshal(plain(np))
}
