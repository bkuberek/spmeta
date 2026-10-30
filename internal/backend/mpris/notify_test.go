package mpris

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
)

// signalBus is a fake bus that also delivers player-change signals.
type signalBus struct {
	*fakeBus
	wake  chan struct{}
	err   error
	block bool
}

func (s *signalBus) Signals(ctx context.Context) (<-chan struct{}, error) {
	if s.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.wake, nil
}

// Changes is what watch mode looks for on the local backend.
var _ backend.Notifier = (*Backend)(nil)

func TestChangesForwardsSignalsUntilContextEnds(t *testing.T) {
	t.Parallel()
	bus := &signalBus{fakeBus: newFakeBus(nil), wake: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := newBackend(bus, "").Changes(ctx)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if ch != (<-chan struct{})(bus.wake) {
		t.Fatal("Changes did not return the bus signals")
	}
	if n := bus.closed.Load(); n != 0 {
		t.Fatalf("connection closed %d times while listening", n)
	}

	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for bus.closed.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("connection not closed after ctx ended")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestChangesErrors(t *testing.T) {
	t.Parallel()
	noBus := &backend.Error{Kind: backend.KindNotAvailable, Message: "no D-Bus session bus found"}
	refused := errors.New("AddMatch refused")
	tests := []struct {
		name    string
		bus     Bus
		dialErr error
		want    func(error) bool
	}{
		{
			name:    "no session bus passes through",
			dialErr: noBus,
			want:    func(err error) bool { return errors.Is(err, noBus) },
		},
		{
			name:    "unreachable bus is not available",
			dialErr: errors.New("connection refused"),
			want: func(err error) bool {
				var be *backend.Error
				return errors.As(err, &be) && be.Kind == backend.KindNotAvailable
			},
		},
		{
			name: "a bus without signals",
			bus:  newFakeBus(nil),
			want: func(err error) bool { return err != nil },
		},
		{
			name: "a refused subscription",
			bus:  &signalBus{fakeBus: newFakeBus(nil), err: refused},
			want: func(err error) bool { return errors.Is(err, refused) },
		},
		{
			name: "a subscription that does not answer",
			bus:  &signalBus{fakeBus: newFakeBus(nil), block: true},
			want: func(err error) bool { return errors.Is(err, context.DeadlineExceeded) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dial := func(context.Context) (Bus, error) {
				if tt.dialErr != nil {
					return nil, tt.dialErr
				}
				return tt.bus, nil
			}
			b := New(dial, "", time.Now)
			b.timeouts.subscribe = 20 * time.Millisecond
			ch, err := b.Changes(context.Background())
			if ch != nil || !tt.want(err) {
				t.Errorf("Changes = %v, %v", ch, err)
			}
			if tt.bus != nil {
				if n := closedCount(tt.bus); n != 1 {
					t.Errorf("connection closed %d times, want 1", n)
				}
			}
		})
	}
}

func closedCount(b Bus) int32 {
	switch f := b.(type) {
	case *signalBus:
		return f.closed.Load()
	case *fakeBus:
		return f.closed.Load()
	}
	return -1
}
