package mpris

import (
	"context"
	"slices"
	"strings"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

// preferred is the order in which players are tried when --player is not set.
var preferred = []string{"spotify", "spotifyd", "ncspot", "spotify_player"}

type selection struct {
	suffix string
	props  Properties
}

type getFunc func(ctx context.Context, suffix string) (Properties, error)

// suffixes reduces bus names to the sorted, distinct suffixes of MPRIS players.
func suffixes(names []string) []string {
	var out []string
	for _, n := range names {
		if s, ok := strings.CutPrefix(n, busPrefix); ok && s != "" {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// matches applies playerctl's rule: the suffix equals name or continues it
// after a dot, so "spotify" matches "spotify.instance1" but not "spotifyd".
func matches(name, suffix string) bool {
	if suffix == name {
		return true
	}
	rest, ok := strings.CutPrefix(suffix, name)
	return ok && strings.HasPrefix(rest, ".")
}

// choose picks the player Read should report. Players whose GetAll fails are
// skipped. It returns ctx's error as soon as ctx ends, and a *backend.Error
// when nothing can be selected.
func choose(ctx context.Context, all []string, player string, get getFunc) (selection, error) {
	entries := preferred
	if player != "" {
		entries = []string{player}
	}
	var failed bool
	for _, entry := range entries {
		var first *selection
		for _, suffix := range all {
			if !matches(entry, suffix) {
				continue
			}
			if err := ctx.Err(); err != nil {
				return selection{}, err
			}
			props, err := get(ctx, suffix)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return selection{}, ctxErr
				}
				failed = true
				continue
			}
			sel := selection{suffix: suffix, props: props}
			if status, _ := props["PlaybackStatus"].(string); status == "Playing" {
				return sel, nil
			}
			if first == nil {
				first = &sel
			}
		}
		if first != nil {
			return *first, nil
		}
	}
	switch {
	case failed:
		return selection{}, notAvailable("MPRIS players did not answer", "")
	case player != "":
		return selection{}, notAvailable("no MPRIS player named "+player, "Run `spmeta doctor` to list players.")
	default:
		return selection{}, notAvailable("no Spotify MPRIS player found",
			"Start Spotify, spotifyd, ncspot, or spotify_player, or pass --player.")
	}
}

func notAvailable(msg, hint string) *backend.Error {
	return &backend.Error{Kind: backend.KindNotAvailable, Source: model.SourceMPRIS, Message: msg, Hint: hint}
}
