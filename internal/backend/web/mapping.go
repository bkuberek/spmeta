package web

import (
	"encoding/json"
	"strings"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

const (
	itemTrack   = "track"
	itemEpisode = "episode"
	typeAd      = "ad"
	artistsSep  = ", "
	httpsPrefix = "https://"
)

// player is the part of the GET /v1/me/player response spmeta reads. Unknown
// fields are ignored.
type player struct {
	IsPlaying            bool    `json:"is_playing"`
	ProgressMS           *int64  `json:"progress_ms"`
	CurrentlyPlayingType string  `json:"currently_playing_type"`
	Device               *device `json:"device"`
	Item                 *item   `json:"item"`
}

type device struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	VolumePercent *int   `json:"volume_percent"`
}

// item is a track, an episode, or something else; Type tells which.
type item struct {
	Type         string  `json:"type"`
	Name         string  `json:"name"`
	URI          string  `json:"uri"`
	DurationMS   *int64  `json:"duration_ms"`
	Explicit     *bool   `json:"explicit"`
	TrackNumber  *int    `json:"track_number"`
	DiscNumber   *int    `json:"disc_number"`
	Artists      []named `json:"artists"`
	Album        *album  `json:"album"`
	Show         *show   `json:"show"`
	Images       []image `json:"images"`
	ExternalURLs struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
	ExternalIDs struct {
		ISRC string `json:"isrc"`
	} `json:"external_ids"`
}

type named struct {
	Name string `json:"name"`
}

type album struct {
	Name    string  `json:"name"`
	Artists []named `json:"artists"`
	Images  []image `json:"images"`
}

type show struct {
	Name      string  `json:"name"`
	Publisher string  `json:"publisher"`
	Images    []image `json:"images"`
}

type image struct {
	URL    string `json:"url"`
	Width  *int   `json:"width"`
	Height *int   `json:"height"`
}

// decode parses the body of a 200 response.
func decode(body []byte) (player, error) {
	var p player
	if err := json.Unmarshal(body, &p); err != nil {
		return player{}, &backend.Error{
			Kind:    backend.KindUnexpected,
			Source:  model.SourceWeb,
			Message: "unexpected response from the Spotify Web API",
			Err:     err,
		}
	}
	return p, nil
}

// mapPlayer converts a response into a raw NowPlaying. A null item means
// nothing is playing, except during an ad, which has the item null as well.
func mapPlayer(p player) (model.NowPlaying, error) {
	isAd := p.CurrentlyPlayingType == typeAd
	if p.Item == nil && !isAd {
		return model.NowPlaying{}, errNothingPlaying()
	}

	np := model.NowPlaying{
		Source:     model.SourceWeb,
		State:      state(p.IsPlaying),
		PositionMS: p.ProgressMS,
		Device:     mapDevice(p.Device),
	}
	if p.Item != nil {
		mapItem(&np, p.Item)
	}
	if isAd {
		np.Type = model.ItemAd
	}
	return np, nil
}

func state(playing bool) model.State {
	if playing {
		return model.StatePlaying
	}
	return model.StatePaused
}

func mapDevice(d *device) *model.Device {
	if d == nil {
		return nil
	}
	return &model.Device{Name: d.Name, Type: d.Type, VolumePercent: d.VolumePercent}
}

// mapItem fills the fields of np that come from the item. An item that is
// neither a track nor an episode keeps only the common fields.
func mapItem(np *model.NowPlaying, it *item) {
	np.Title = &it.Name
	np.URI = &it.URI
	np.DurationMS = it.DurationMS

	switch it.Type {
	case itemTrack:
		np.Type = model.ItemTrack
		mapTrack(np, it)
	case itemEpisode:
		np.Type = model.ItemEpisode
		mapEpisode(np, it)
	default:
		np.Type = model.ItemUnknown
	}
}

func mapTrack(np *model.NowPlaying, it *item) {
	np.Artists = names(it.Artists)
	np.TrackNumber = it.TrackNumber
	np.DiscNumber = it.DiscNumber
	np.URL = &it.ExternalURLs.Spotify
	np.ISRC = &it.ExternalIDs.ISRC
	np.Explicit = it.Explicit
	if it.Album != nil {
		np.Album = &it.Album.Name
		joined := strings.Join(names(it.Album.Artists), artistsSep)
		np.AlbumArtist = &joined
		np.ArtworkURL = largestImage(it.Album.Images)
	}
}

func mapEpisode(np *model.NowPlaying, it *item) {
	np.URL = &it.ExternalURLs.Spotify
	np.Explicit = it.Explicit
	np.ArtworkURL = largestImage(it.Images)
	if it.Show != nil {
		np.Artists = []string{it.Show.Publisher}
		np.Album = &it.Show.Name
		if np.ArtworkURL == nil {
			np.ArtworkURL = largestImage(it.Show.Images)
		}
	}
}

func names(ns []named) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Name
	}
	return out
}

// largestImage returns the URL of the image with the greatest width times
// height. An image without both dimensions counts as zero, so when none has
// dimensions, or on a tie, the first one wins (Spotify lists the largest
// first). Entries that are not HTTPS URLs are skipped, because artwork_url must
// be one.
func largestImage(images []image) *string {
	var best *image
	var bestArea int64 = -1
	for i := range images {
		img := &images[i]
		if !strings.HasPrefix(img.URL, httpsPrefix) {
			continue
		}
		if area := img.area(); area > bestArea {
			best, bestArea = img, area
		}
	}
	if best == nil {
		return nil
	}
	return &best.URL
}

func (i image) area() int64 {
	if i.Width == nil || i.Height == nil {
		return 0
	}
	return int64(*i.Width) * int64(*i.Height)
}
