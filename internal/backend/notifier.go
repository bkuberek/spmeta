package backend

import "context"

// Notifier is implemented by a backend that can announce that the current
// item may have changed, so that watch mode reads at once instead of waiting
// for the next poll. Polling stays the fallback.
type Notifier interface {
	// Changes starts listening and returns a channel that receives a value
	// when the item may have changed. At most one value is pending, so a
	// burst of changes costs one read. The channel closes when ctx ends or
	// the listener stops. An error means nothing was started. Changes may
	// run concurrently with Read.
	Changes(ctx context.Context) (<-chan struct{}, error)
}

// Coalesce turns each value received from in that keep accepts (every value
// when keep is nil) into a wake on the returned channel, which holds at most
// one pending wake: values that arrive while one is pending are dropped. It
// drains in promptly and closes the returned channel once in closes.
func Coalesce[T any](in <-chan T, keep func(T) bool) <-chan struct{} {
	out := make(chan struct{}, 1)
	go func() {
		defer close(out)
		for v := range in {
			if keep != nil && !keep(v) {
				continue
			}
			select {
			case out <- struct{}{}:
			default:
			}
		}
	}()
	return out
}
