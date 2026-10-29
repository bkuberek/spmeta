//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package auth_test

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/auth"
)

const (
	childAccountsEnv = "SPMETA_TEST_ACCOUNTS_URL"
	childPathEnv     = "SPMETA_TEST_TOKEN_PATH"
	childRefreshers  = 4
)

// TestRefresherChild is the body of the child processes started by
// TestLockAcrossProcessesAllowsOneRefresh; it does nothing in a normal run.
func TestRefresherChild(t *testing.T) {
	url, path := os.Getenv(childAccountsEnv), os.Getenv(childPathEnv)
	if url == "" || path == "" {
		t.Skip("helper for TestLockAcrossProcessesAllowsOneRefresh")
	}
	var announce sync.Once
	src := &auth.TokenSource{
		Path:      path,
		Endpoints: auth.Endpoints{Accounts: url},
		Now:       func() time.Time { return epoch },
		Sleep: func(ctx context.Context, d time.Duration) error {
			announce.Do(func() { fmt.Println("waiting") })
			time.Sleep(d)
			return ctx.Err()
		},
	}
	tok, err := src.Token(context.Background(), false)
	if err != nil {
		fmt.Println("error")
		os.Exit(1)
	}
	fmt.Println("token=" + tok)
	os.Exit(0)
}

// TestLockAcrossProcessesAllowsOneRefresh starts real processes over one token
// file. The first to take the flock stays inside its HTTP request until every
// other process has reported that it is waiting for the lock, so the overlap
// does not depend on timing. All of them must end up with the refreshed token
// after a single request.
func TestLockAcrossProcessesAllowsOneRefresh(t *testing.T) {
	t.Parallel()
	body := fixture(t, "token-exchange.json")
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }

	r := newRig(t, 0, func(w http.ResponseWriter, req *http.Request) {
		<-release
		respond(200, body)(w, req)
	})
	t.Cleanup(unblock)

	waiting := make(chan struct{}, childRefreshers)
	tokens := make(chan string, childRefreshers)
	var wg sync.WaitGroup
	for range childRefreshers {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRefresherChild$")
		cmd.Env = append(os.Environ(), childAccountsEnv+"="+r.acc.srv.URL, childPathEnv+"="+r.path)
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sc := bufio.NewScanner(out)
			for sc.Scan() {
				switch line := sc.Text(); {
				case line == "waiting":
					waiting <- struct{}{}
				case strings.HasPrefix(line, "token="):
					tokens <- strings.TrimPrefix(line, "token=")
				}
			}
			if err := cmd.Wait(); err != nil {
				t.Errorf("refresher process: %v", err)
			}
		}()
	}

	for range childRefreshers - 1 {
		select {
		case <-waiting:
		case <-time.After(stuckGuard):
			t.Fatal("a refresher process never waited for the lock")
		}
	}
	unblock()
	wg.Wait()
	close(tokens)

	n := 0
	for tok := range tokens {
		n++
		if tok != newAccess {
			t.Errorf("a refresher ended with %q, want the refreshed token", tok)
		}
	}
	if n != childRefreshers {
		t.Errorf("%d refreshers reported a token, want %d", n, childRefreshers)
	}
	if got := r.acc.requests.Load(); got != 1 {
		t.Errorf("token requests = %d, want exactly 1", got)
	}
}
