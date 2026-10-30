package mpris

import (
	"context"
	"errors"
	"fmt"
)

// Changes implements backend.Notifier. It opens a connection of its own,
// separate from the one each Read opens, and keeps it until ctx ends. The
// channel receives a value when any player's Player properties change or an
// MPRIS player appears or quits. It does not filter by player: Read still
// selects one, and the watch loop prints nothing when the output is
// unchanged. The channel closes when the connection drops. Changes may run
// concurrently with Read.
func (b *Backend) Changes(ctx context.Context) (<-chan struct{}, error) {
	bus, err := b.connect(ctx)
	if err != nil {
		return nil, b.wrap(ctx, err)
	}
	s, ok := bus.(signaler)
	if !ok {
		_ = bus.Close()
		return nil, errors.New("the session bus connection delivers no signals")
	}
	sctx, cancel := context.WithTimeout(ctx, b.timeouts.subscribe)
	defer cancel()
	wake, err := s.Signals(sctx)
	if err != nil {
		_ = bus.Close()
		return nil, fmt.Errorf("subscribe to MPRIS signals: %w", err)
	}
	context.AfterFunc(ctx, func() { _ = bus.Close() })
	return wake, nil
}
