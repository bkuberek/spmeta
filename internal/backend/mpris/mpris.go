package mpris

import (
	"context"
	"errors"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

const (
	// spotifyDesktop is the bus suffix of the official client, whose Position
	// is often stuck at 0.
	spotifyDesktop = "spotify"
	hintNoBus      = "Run spmeta inside a desktop session, or with `dbus-run-session`."
)

type timeouts struct {
	connect, list, getAll, total time.Duration
}

var defaultTimeouts = timeouts{
	connect: 500 * time.Millisecond,
	list:    500 * time.Millisecond,
	getAll:  500 * time.Millisecond,
	total:   2 * time.Second,
}

// Backend reads the current item from an MPRIS player. It keeps Position
// history between reads and is not safe for concurrent use.
type Backend struct {
	dial      DialFunc
	player    string
	now       func() time.Time
	timeouts  timeouts
	positions map[string]positionState
}

type positionState struct {
	last    int64
	trusted bool
}

// PlayerInfo describes one MPRIS player found on the bus.
type PlayerInfo struct {
	Name     string // bus name suffix, for example "spotifyd.instance1234"
	Status   string // PlaybackStatus; empty when the player did not answer
	Selected bool   // the player Read would report
}

// New returns a Backend that connects with dial and stamps results with now.
// player is the --player value; empty selects among the Spotify clients.
func New(dial DialFunc, player string, now func() time.Time) *Backend {
	return &Backend{
		dial:      dial,
		player:    player,
		now:       now,
		timeouts:  defaultTimeouts,
		positions: make(map[string]positionState),
	}
}

// Source identifies this backend.
func (b *Backend) Source() model.Source { return model.SourceMPRIS }

// Read returns the current item from the selected player. Each call opens
// and closes its own bus connection.
func (b *Backend) Read(ctx context.Context) (model.NowPlaying, error) {
	ctx, cancel := context.WithTimeout(ctx, b.timeouts.total)
	defer cancel()

	bus, all, err := b.open(ctx)
	if err != nil {
		return model.NowPlaying{}, err
	}
	defer func() { _ = bus.Close() }()

	sel, err := choose(ctx, all, b.player, b.getter(bus))
	if err != nil {
		return model.NowPlaying{}, b.wrap(ctx, err)
	}
	np, err := mapProperties(sel.suffix, sel.props)
	if err != nil {
		return model.NowPlaying{}, err
	}
	if sel.suffix == spotifyDesktop {
		b.applyPositionRule(busPrefix+sel.suffix, sel.props, &np)
	}
	np.FetchedAt = b.now()
	return model.Normalize(np), nil
}

// Players lists every MPRIS player on the bus, sorted by name, and marks the
// one Read would report. It is used by doctor.
func (b *Backend) Players(ctx context.Context) ([]PlayerInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, b.timeouts.total)
	defer cancel()

	bus, all, err := b.open(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = bus.Close() }()

	answers := make(map[string]Properties, len(all))
	get := b.getter(bus)
	for _, s := range all {
		if p, err := get(ctx, s); err == nil {
			answers[s] = p
		} else if ctx.Err() != nil {
			return nil, b.ctxError(ctx)
		}
	}
	cached := func(_ context.Context, s string) (Properties, error) {
		if p, ok := answers[s]; ok {
			return p, nil
		}
		return nil, errors.New("no answer")
	}
	sel, _ := choose(ctx, all, b.player, cached)

	infos := make([]PlayerInfo, 0, len(all))
	for _, s := range all {
		status, _ := answers[s]["PlaybackStatus"].(string)
		infos = append(infos, PlayerInfo{Name: s, Status: status, Selected: s == sel.suffix})
	}
	return infos, nil
}

// open connects and lists the MPRIS player suffixes. The caller closes bus.
func (b *Backend) open(ctx context.Context) (Bus, []string, error) {
	bus, err := b.connect(ctx)
	if err != nil {
		return nil, nil, b.wrap(ctx, err)
	}
	lctx, cancel := context.WithTimeout(ctx, b.timeouts.list)
	defer cancel()
	names, err := bus.ListNames(lctx)
	if err != nil {
		_ = bus.Close()
		return nil, nil, b.wrap(ctx, err)
	}
	return bus, suffixes(names), nil
}

type dialResult struct {
	bus Bus
	err error
}

// connect bounds the dial by the connect timeout without cancelling the
// context the connection may live on. A connection that arrives after the
// timeout is closed.
func (b *Backend) connect(ctx context.Context) (Bus, error) {
	ch := make(chan dialResult, 1)
	go func() {
		bus, err := b.dial(ctx)
		ch <- dialResult{bus, err}
	}()
	timer := time.NewTimer(b.timeouts.connect)
	defer timer.Stop()

	var abandon error
	select {
	case r := <-ch:
		return r.bus, r.err
	case <-timer.C:
		abandon = context.DeadlineExceeded
	case <-ctx.Done():
		abandon = ctx.Err()
	}
	go func() {
		if r := <-ch; r.bus != nil {
			_ = r.bus.Close()
		}
	}()
	return nil, abandon
}

func (b *Backend) getter(bus Bus) getFunc {
	return func(ctx context.Context, suffix string) (Properties, error) {
		ctx, cancel := context.WithTimeout(ctx, b.timeouts.getAll)
		defer cancel()
		return bus.GetAll(ctx, busPrefix+suffix)
	}
}

// applyPositionRule hides a Position of 0 reported by Spotify desktop while
// playing, because it reports 0 regardless of progress. Once two reads of the
// same bus name return different nonzero positions, the player is trusted.
func (b *Backend) applyPositionRule(busName string, p Properties, np *model.NowPlaying) {
	pos, ok := asInt64(p["Position"])
	if !ok {
		return
	}
	st, seen := b.positions[busName]
	if seen && pos != 0 && pos != st.last {
		st.trusted = true
	}
	st.last = pos
	b.positions[busName] = st
	if np.State == model.StatePlaying && pos == 0 && !st.trusted {
		np.PositionMS = nil
	}
}

// wrap maps an error from the bus or from selection to a *backend.Error.
func (b *Backend) wrap(ctx context.Context, err error) error {
	var be *backend.Error
	switch {
	case errors.As(err, &be):
		return be
	case ctx.Err() != nil:
		return b.ctxError(ctx)
	default:
		return &backend.Error{
			Kind:    backend.KindNotAvailable,
			Source:  model.SourceMPRIS,
			Message: "could not reach the D-Bus session bus",
			Hint:    hintNoBus,
			Err:     err,
		}
	}
}

func (b *Backend) ctxError(ctx context.Context) *backend.Error {
	msg := "read cancelled"
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		msg = "MPRIS read timed out"
	}
	return &backend.Error{Kind: backend.KindUnexpected, Source: model.SourceMPRIS, Message: msg, Err: ctx.Err()}
}
