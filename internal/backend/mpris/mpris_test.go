package mpris

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

var _ backend.Backend = (*Backend)(nil)

func TestSource(t *testing.T) {
	if got := newBackend(newFakeBus(nil), "").Source(); got != model.SourceMPRIS {
		t.Errorf("Source = %q, want mpris", got)
	}
}

func TestReadStampsFetchedAtAndClosesBus(t *testing.T) {
	bus := newFakeBus(map[string]fakePlayer{"spotifyd": ok(playerProps("Playing"))})
	np, err := newBackend(bus, "").Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !np.FetchedAt.Equal(fetchedAt) || np.SchemaVersion != model.SchemaVersion {
		t.Errorf("got fetched_at %v schema %d, want normalized output at %v", np.FetchedAt, np.SchemaVersion, fetchedAt)
	}
	if bus.closed.Load() != 1 {
		t.Errorf("bus closed %d times, want 1", bus.closed.Load())
	}
}

func TestReadClosesBusOnError(t *testing.T) {
	bus := newFakeBus(nil)
	if _, err := newBackend(bus, "").Read(context.Background()); err == nil {
		t.Fatal("want an error")
	}
	if bus.closed.Load() != 1 {
		t.Errorf("bus closed %d times, want 1", bus.closed.Load())
	}
}

func TestReadSkipsLowerPreferenceOnceFound(t *testing.T) {
	bus := newFakeBus(map[string]fakePlayer{
		"spotify": ok(playerProps("Playing")),
		"ncspot":  ok(playerProps("Playing")),
	})
	if _, err := newBackend(bus, "").Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"spotify"}; !reflect.DeepEqual(bus.getCalls, want) {
		t.Errorf("GetAll calls = %v, want %v", bus.getCalls, want)
	}
}

func spotifyProps(status string, position int64) Properties {
	return props(status, position, metadata(map[string]any{"mpris:trackid": "/com/spotify/track/" + trackID}))
}

func positionOf(t *testing.T, b *Backend) *int64 {
	t.Helper()
	np, err := b.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return np.PositionMS
}

func TestSpotifyDesktopPositionRule(t *testing.T) {
	const second = int64(1_000_000)
	tests := []struct {
		name    string
		suffix  string
		status  string
		reads   []int64 // Position in microseconds, one per Read
		want    []*int64
		prepare func(p Properties)
	}{
		{"zero while playing is unknown", "spotify", "Playing", []int64{0, 0, 0}, []*int64{nil, nil, nil}, nil},
		{"nonzero first read is used as reported", "spotify", "Playing", []int64{3 * second}, []*int64{ptr(int64(3000))}, nil},
		{
			"change between reads trusts the player", "spotify", "Playing",
			[]int64{0, 5 * second, 0, 7 * second},
			[]*int64{nil, ptr(int64(5000)), ptr(int64(0)), ptr(int64(7000))},
			nil,
		},
		{
			"two nonzero reads trust the player", "spotify", "Playing",
			[]int64{second, 3 * second, 0},
			[]*int64{ptr(int64(1000)), ptr(int64(3000)), ptr(int64(0))},
			nil,
		},
		{
			"same nonzero value does not establish trust", "spotify", "Playing",
			[]int64{second, second, 0},
			[]*int64{ptr(int64(1000)), ptr(int64(1000)), nil},
			nil,
		},
		{"paused zero is reported", "spotify", "Paused", []int64{0}, []*int64{ptr(int64(0))}, nil},
		{"other players report zero as zero", "spotifyd.instance1", "Playing", []int64{0, 0}, []*int64{ptr(int64(0)), ptr(int64(0))}, nil},
		{"instance suffixes are not the desktop client", "spotify.instance2", "Playing", []int64{0}, []*int64{ptr(int64(0))}, nil},
		{"missing position stays null", "spotify", "Playing", []int64{-1}, []*int64{nil}, func(p Properties) { delete(p, "Position") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := spotifyProps(tt.status, 0)
			bus := newFakeBus(map[string]fakePlayer{tt.suffix: ok(p)})
			b := newBackend(bus, "")
			for i, pos := range tt.reads {
				p["Position"] = pos
				if tt.prepare != nil {
					tt.prepare(p)
				}
				got := positionOf(t, b)
				if !reflect.DeepEqual(got, tt.want[i]) {
					t.Errorf("read %d: position_ms = %s, want %s", i+1, fmtPtr(got), fmtPtr(tt.want[i]))
				}
			}
		})
	}
}

func TestPositionStateLivesInTheBackend(t *testing.T) {
	// Trust is remembered by one Backend; a new Backend starts without it.
	p := spotifyProps("Playing", 0)
	bus := newFakeBus(map[string]fakePlayer{"spotify": ok(p)})
	b := newBackend(bus, "")
	positionOf(t, b)
	p["Position"] = int64(2_000_000)
	positionOf(t, b)
	p["Position"] = int64(0)
	if got := positionOf(t, b); got == nil || *got != 0 {
		t.Fatalf("trusted backend: position_ms = %s, want 0", fmtPtr(got))
	}
	if got := positionOf(t, newBackend(bus, "")); got != nil {
		t.Errorf("fresh backend: position_ms = %s, want null", fmtPtr(got))
	}
}

func fmtPtr(p *int64) string {
	if p == nil {
		return "null"
	}
	return time.Duration(*p * int64(time.Millisecond)).String()
}

func TestReadErrors(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name    string
		bus     func() *fakeBus
		dial    DialFunc
		player  string
		kind    backend.Kind
		message string
	}{
		{
			name:    "dial fails",
			dial:    func(context.Context) (Bus, error) { return nil, boom },
			kind:    backend.KindNotAvailable,
			message: "could not reach the D-Bus session bus",
		},
		{
			name:    "ListNames fails",
			bus:     func() *fakeBus { b := newFakeBus(nil); b.namesErr = boom; return b },
			kind:    backend.KindNotAvailable,
			message: "could not reach the D-Bus session bus",
		},
		{
			name:    "no player",
			bus:     func() *fakeBus { return newFakeBus(nil) },
			kind:    backend.KindNotAvailable,
			message: "no Spotify MPRIS player found",
		},
		{
			name:    "named player missing",
			bus:     func() *fakeBus { return newFakeBus(nil) },
			player:  "firefox",
			kind:    backend.KindNotAvailable,
			message: "no MPRIS player named firefox",
		},
		{
			name:    "players do not answer",
			bus:     func() *fakeBus { return newFakeBus(map[string]fakePlayer{"spotify": {err: boom}}) },
			kind:    backend.KindNotAvailable,
			message: "MPRIS players did not answer",
		},
		{
			name: "unknown PlaybackStatus",
			bus: func() *fakeBus {
				return newFakeBus(map[string]fakePlayer{"spotify": ok(spotifyProps("Weird", 0))})
			},
			kind:    backend.KindUnexpected,
			message: `unexpected PlaybackStatus "Weird" from spotify`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dial := tt.dial
			if dial == nil {
				dial = dialer(tt.bus())
			}
			_, err := New(dial, tt.player, time.Now).Read(context.Background())
			var be *backend.Error
			if !errors.As(err, &be) {
				t.Fatalf("err = %v, want *backend.Error", err)
			}
			if be.Kind != tt.kind || be.Message != tt.message || be.Source != model.SourceMPRIS {
				t.Errorf("got kind %v source %q message %q, want %v mpris %q", be.Kind, be.Source, be.Message, tt.kind, tt.message)
			}
		})
	}
}

func TestReadNotAvailableHints(t *testing.T) {
	_, err := newBackend(newFakeBus(nil), "").Read(context.Background())
	var be *backend.Error
	if !errors.As(err, &be) || be.Hint == "" {
		t.Errorf("err = %v, want a hint naming the next step", err)
	}
}

func TestReadKeepsTypedDialError(t *testing.T) {
	want := &backend.Error{Kind: backend.KindNotAvailable, Source: model.SourceMPRIS, Message: "no D-Bus session bus found", Hint: "x"}
	b := New(func(context.Context) (Bus, error) { return nil, want }, "", time.Now)
	_, err := b.Read(context.Background())
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want the dial error unchanged", err)
	}
}

func TestReadTimeouts(t *testing.T) {
	short := 20 * time.Millisecond
	tests := []struct {
		name    string
		bus     func() *fakeBus
		dial    DialFunc
		tune    func(*timeouts)
		kind    backend.Kind
		message string
	}{
		{
			name:    "ListNames timeout is not available",
			bus:     func() *fakeBus { b := newFakeBus(nil); b.blockNames = true; return b },
			tune:    func(to *timeouts) { to.list = short },
			kind:    backend.KindNotAvailable,
			message: "could not reach the D-Bus session bus",
		},
		{
			name: "connect timeout is not available",
			dial: func(ctx context.Context) (Bus, error) { <-ctx.Done(); return nil, ctx.Err() },
			tune: func(to *timeouts) { to.connect = short },
			kind: backend.KindNotAvailable, message: "could not reach the D-Bus session bus",
		},
		{
			name:    "whole read deadline",
			bus:     func() *fakeBus { b := newFakeBus(nil); b.blockNames = true; return b },
			tune:    func(to *timeouts) { to.total = short },
			kind:    backend.KindUnexpected,
			message: "MPRIS read timed out",
		},
		{
			name: "every player timing out is not available",
			bus: func() *fakeBus {
				return newFakeBus(map[string]fakePlayer{"spotify": {block: true}, "ncspot": {block: true}})
			},
			tune:    func(to *timeouts) { to.getAll = short },
			kind:    backend.KindNotAvailable,
			message: "MPRIS players did not answer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dial := tt.dial
			if dial == nil {
				dial = dialer(tt.bus())
			}
			b := New(dial, "", time.Now)
			tt.tune(&b.timeouts)
			start := time.Now()
			_, err := b.Read(context.Background())
			if time.Since(start) > time.Second {
				t.Errorf("Read took %v, want it bounded by the timeout", time.Since(start))
			}
			var be *backend.Error
			if !errors.As(err, &be) || be.Kind != tt.kind || be.Message != tt.message {
				t.Errorf("err = %v, want kind %v %q", err, tt.kind, tt.message)
			}
		})
	}
}

func TestReadCancelled(t *testing.T) {
	bus := newFakeBus(nil)
	bus.blockNames = true
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	_, err := newBackend(bus, "").Read(ctx)
	var be *backend.Error
	if !errors.As(err, &be) || be.Kind != backend.KindUnexpected || be.Message != "read cancelled" {
		t.Errorf("err = %v, want Unexpected read cancelled", err)
	}
}

func TestLateConnectionIsClosed(t *testing.T) {
	late := newFakeBus(nil)
	release := make(chan struct{})
	dial := func(context.Context) (Bus, error) { <-release; return late, nil }
	b := New(dial, "", time.Now)
	b.timeouts.connect = 20 * time.Millisecond
	if _, err := b.Read(context.Background()); backend.KindOf(err) != backend.KindNotAvailable {
		t.Fatalf("err = %v, want not available", err)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if late.closed.Load() == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("a connection that arrived after the timeout was not closed")
}

func TestPlayers(t *testing.T) {
	bus := newFakeBus(map[string]fakePlayer{
		"firefox.instance1":  ok(playerProps("Playing")),
		"spotifyd.instance1": ok(playerProps("Paused")),
		"spotifyd.instance2": ok(playerProps("Playing")),
		"ncspot":             {err: errors.New("boom")},
	}, ":1.5", "org.freedesktop.DBus")
	got, err := newBackend(bus, "").Players(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []PlayerInfo{
		{Name: "firefox.instance1", Status: "Playing"},
		{Name: "ncspot"},
		{Name: "spotifyd.instance1", Status: "Paused"},
		{Name: "spotifyd.instance2", Status: "Playing", Selected: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Players = %+v\nwant      %+v", got, want)
	}
	if bus.closed.Load() != 1 {
		t.Errorf("bus closed %d times, want 1", bus.closed.Load())
	}
}

func TestPlayersWithNamedPlayerAndNoMatch(t *testing.T) {
	bus := newFakeBus(map[string]fakePlayer{"spotify": ok(playerProps("Playing"))})
	got, err := newBackend(bus, "firefox").Players(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []PlayerInfo{{Name: "spotify", Status: "Playing"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Players = %+v, want %+v", got, want)
	}
}

func TestPlayersBusUnreachable(t *testing.T) {
	bus := newFakeBus(nil)
	bus.namesErr = errors.New("boom")
	_, err := newBackend(bus, "").Players(context.Background())
	if backend.KindOf(err) != backend.KindNotAvailable {
		t.Errorf("err = %v, want not available", err)
	}
}
