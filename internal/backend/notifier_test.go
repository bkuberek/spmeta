package backend_test

import (
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/backend"
)

func TestCoalesce(t *testing.T) {
	t.Parallel()

	t.Run("a burst becomes one pending wake", func(t *testing.T) {
		t.Parallel()
		in := make(chan string)
		// Values are handled in order, so once keep sees "end" every value
		// of the burst has been handled.
		handled := make(chan struct{})
		out := backend.Coalesce(in, func(s string) bool {
			if s == "end" {
				close(handled)
				return false
			}
			return true
		})
		for _, s := range []string{"Metadata", "PlaybackStatus", "Metadata", "end"} {
			in <- s
		}
		<-handled
		if _, ok := <-out; !ok {
			t.Fatal("no wake after a burst")
		}
		select {
		case <-out:
			t.Error("a second wake from one burst")
		default:
		}
		close(in)
	})

	t.Run("a wake per value once the previous was taken", func(t *testing.T) {
		t.Parallel()
		in := make(chan int)
		out := backend.Coalesce(in, nil)
		for i := range 3 {
			in <- i
			select {
			case <-out:
			case <-time.After(3 * time.Second):
				t.Fatalf("no wake for value %d", i)
			}
		}
		close(in)
	})

	t.Run("values the filter rejects do not wake", func(t *testing.T) {
		t.Parallel()
		in := make(chan string)
		out := backend.Coalesce(in, func(s string) bool { return s != "NameAcquired" })
		in <- "NameAcquired"
		close(in)
		if _, ok := <-out; ok {
			t.Error("woken by a rejected value")
		}
	})

	t.Run("the output closes after the input", func(t *testing.T) {
		t.Parallel()
		in := make(chan struct{})
		out := backend.Coalesce(in, nil)
		close(in)
		select {
		case _, ok := <-out:
			if ok {
				t.Error("wake without a value")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("output did not close")
		}
	})
}
