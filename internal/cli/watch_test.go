package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/cli"
	"github.com/bkuberek/spmeta/internal/config"
	"github.com/bkuberek/spmeta/internal/model"
	"github.com/bkuberek/spmeta/internal/watch"
	"github.com/bkuberek/spmeta/schema"
)

type wstep struct {
	np  model.NowPlaying
	err error
}

// scriptedBackend replays steps and cancels the run when they are used up.
type scriptedBackend struct {
	src    model.Source
	steps  []wstep
	calls  int
	cancel context.CancelFunc
}

func (b *scriptedBackend) Source() model.Source { return b.src }

func (b *scriptedBackend) Read(ctx context.Context) (model.NowPlaying, error) {
	if b.calls >= len(b.steps) {
		b.cancel()
		return model.NowPlaying{}, ctx.Err()
	}
	st := b.steps[b.calls]
	b.calls++
	return st.np, st.err
}

type watchRun struct {
	code           int
	stdout, stderr string
	sleeps         []time.Duration
	local, web     *scriptedBackend
}

// notifyingBackend adds change notifications to a scripted backend.
type notifyingBackend struct {
	*scriptedBackend
	changes func(ctx context.Context) (<-chan struct{}, error)
}

func (b notifyingBackend) Changes(ctx context.Context) (<-chan struct{}, error) {
	return b.changes(ctx)
}

type watchSetup struct {
	local, web []wstep
	vars       map[string]string
	files      map[string]string
	stdout     func(*bytes.Buffer) io.Writer
	// changes, when set, makes the local backend a backend.Notifier.
	changes func(ctx context.Context) (<-chan struct{}, error)
}

func runWatch(t *testing.T, s watchSetup, args ...string) watchRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := watchRun{local: &scriptedBackend{src: model.SourceMacOS, steps: s.local, cancel: cancel}}
	var local backend.Backend = r.local
	if s.changes != nil {
		local = notifyingBackend{r.local, s.changes}
	}
	opts := []cli.Option{cli.WithBackend(local)}
	if s.web != nil {
		r.web = &scriptedBackend{src: model.SourceWeb, steps: s.web, cancel: cancel}
		opts = append(opts, cli.WithWebBackend(r.web))
	}
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	opts = append(opts,
		cli.WithEnv(config.Env{
			Getenv: func(k string) string { return s.vars[k] },
			Home:   testHome,
			ReadFile: func(p string) ([]byte, error) {
				if c, ok := s.files[p]; ok {
					return []byte(c), nil
				}
				return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
			},
		}),
		cli.WithClock(func() time.Time { return now }),
		cli.WithSleep(func(ctx context.Context, d time.Duration) error {
			r.sleeps = append(r.sleeps, d)
			now = now.Add(d)
			return ctx.Err()
		}),
	)
	var out, errOut bytes.Buffer
	var stdout io.Writer = &out
	if s.stdout != nil {
		stdout = s.stdout(&out)
	}
	r.code = cli.Run(ctx, args, stdout, &errOut, cli.BuildInfo{Version: "dev"}, opts...)
	r.stdout, r.stderr = out.String(), errOut.String()
	return r
}

// A source that can never become available in this run is a configuration
// error: watch mode must exit at once instead of polling silently forever.
func TestWatchWebWithoutLoginExitsImmediately(t *testing.T) {
	r := runWatch(t, watchSetup{}, "--watch", "--source", "web")
	if r.code != 4 {
		t.Errorf("exit = %d, want 4", r.code)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want nothing", r.stdout)
	}
	if r.stderr != notLoggedIn {
		t.Errorf("stderr = %q, want %q", r.stderr, notLoggedIn)
	}
	if len(r.sleeps) != 0 {
		t.Errorf("slept %v, want no polling", r.sleeps)
	}
}

func titled(title string, state model.State) wstep {
	np := playing()
	np.Title, np.State = ptr(title), state
	return wstep{np: np}
}

func TestWatchTextEmitsOnChangeOnly(t *testing.T) {
	moved := playing()
	moved.PositionMS = ptr(int64(130000))
	r := runWatch(t, watchSetup{local: []wstep{
		{np: playing()},
		{np: moved},
		{np: moved},
		titled("Second", model.StatePlaying),
		titled("Second", model.StatePaused),
		{err: errNotRunning},
		{err: errNotRunning},
		{np: playing()},
	}}, "--watch")
	want := "▶ Amazone — Alvan · Home (2:00 / 3:58)\n" +
		"▶ Second — Alvan · Home (2:00 / 3:58)\n" +
		"⏸ Second — Alvan · Home (2:00 / 3:58)\n" +
		"\n" +
		"▶ Amazone — Alvan · Home (2:00 / 3:58)\n"
	if r.code != 0 || r.stdout != want || r.stderr != "" {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 0, stdout %q, no stderr", r.code, r.stdout, r.stderr, want)
	}
}

func TestWatchTemplate(t *testing.T) {
	r := runWatch(t, watchSetup{local: []wstep{
		{np: playing()}, {err: &backend.Error{Kind: backend.KindNothingPlaying, Message: "nothing"}},
	}}, "--watch", "--template", "{artist} - {title}")
	if want := "Alvan - Amazone\n\n"; r.code != 0 || r.stdout != want {
		t.Errorf("exit %d, stdout %q; want exit 0, stdout %q", r.code, r.stdout, want)
	}
}

func TestWatchWaybar(t *testing.T) {
	paused := playing()
	paused.State = model.StatePaused
	r := runWatch(t, watchSetup{local: []wstep{
		{err: errNotRunning},
		{np: playing()},
		{np: paused},
		{err: &backend.Error{Kind: backend.KindNothingPlaying, Message: "nothing"}},
		{np: playing()},
	}}, "--watch", "--format", "waybar")
	const none = `{"text":"","alt":"none","class":"none"}` + "\n"
	const tooltip = `"tooltip":"Amazone\nAlvan\nHome\nSource: macos"`
	want := none +
		`{"text":"▶ Amazone — Alvan · Home (2:00 / 3:58)","alt":"playing",` + tooltip + `,"class":"playing","percentage":50}` + "\n" +
		`{"text":"⏸ Amazone — Alvan · Home (2:00 / 3:58)","alt":"paused",` + tooltip + `,"class":"paused","percentage":50}` + "\n" +
		none +
		`{"text":"▶ Amazone — Alvan · Home (2:00 / 3:58)","alt":"playing",` + tooltip + `,"class":"playing","percentage":50}` + "\n"
	if r.code != 0 || r.stdout != want || r.stderr != "" {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 0, stdout %q, no stderr", r.code, r.stdout, r.stderr, want)
	}
}

func TestWatchNDJSON(t *testing.T) {
	amp := playing()
	amp.Title = ptr("Fish & <Chips>")
	r := runWatch(t, watchSetup{local: []wstep{
		{err: errNotRunning}, {np: amp}, {np: playing()}, {err: errNotRunning},
	}}, "--watch", "--format", "json")
	if r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	lines := strings.Split(strings.TrimSuffix(r.stdout, "\n"), "\n")
	if len(lines) != 5-1 {
		t.Fatalf("got %d lines, want 4:\n%s", len(lines), r.stdout)
	}
	if lines[0] != "null" || lines[3] != "null" {
		t.Errorf("nothing playing lines = %q, %q; want null", lines[0], lines[3])
	}
	if !strings.Contains(lines[1], `"title":"Fish & <Chips>"`) {
		t.Errorf("line %q lost the literal characters; NDJSON must use format.JSON", lines[1])
	}
	sch := compileOutputSchema(t)
	for i, line := range lines {
		doc, err := jsonschema.UnmarshalJSON(strings.NewReader(line))
		if err != nil {
			t.Fatalf("line %d is not JSON: %v", i, err)
		}
		if i == 0 || i == 3 {
			continue
		}
		if err := sch.Validate(doc); err != nil {
			t.Errorf("line %d violates the schema: %v", i, err)
		}
	}
}

func compileOutputSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	const id = "https://raw.githubusercontent.com/bkuberek/spmeta/main/schema/spmeta.schema.json"
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.JSON))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource(id, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type recordingWriter struct {
	mu     sync.Mutex
	buf    *bytes.Buffer
	writes []string
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = append(w.writes, string(p))
	return w.buf.Write(p)
}

func TestWatchWritesOneLinePerWrite(t *testing.T) {
	var rec *recordingWriter
	r := runWatch(t, watchSetup{
		local: []wstep{{np: playing()}, titled("B", model.StatePlaying), {err: errNotRunning}},
		stdout: func(b *bytes.Buffer) io.Writer {
			rec = &recordingWriter{buf: b}
			return rec
		},
	}, "--watch")
	if r.code != 0 {
		t.Fatalf("exit %d", r.code)
	}
	if len(rec.writes) != 3 {
		t.Fatalf("writes = %q, want one per line", rec.writes)
	}
	for _, w := range rec.writes {
		if strings.Count(w, "\n") != 1 || !strings.HasSuffix(w, "\n") {
			t.Errorf("write %q is not exactly one line", w)
		}
	}
}

func TestWatchIntervals(t *testing.T) {
	two := []wstep{{np: playing()}, {np: playing()}}
	tests := []struct {
		name string
		set  watchSetup
		args []string
		want time.Duration
	}{
		{"local default", watchSetup{local: two}, []string{"--watch"}, time.Second},
		{"local flag", watchSetup{local: two}, []string{"--watch", "--interval", "0.5"}, 500 * time.Millisecond},
		{"local flag decimal", watchSetup{local: two}, []string{"--watch", "--interval", "2.25"}, 2250 * time.Millisecond},
		{"local config", watchSetup{local: two, files: map[string]string{defaultConf: "interval = 2"}}, []string{"--watch"}, 2 * time.Second},
		{"flag over config", watchSetup{local: two, files: map[string]string{defaultConf: "interval = 2"}}, []string{"--watch", "--interval", "4"}, 4 * time.Second},
		{"auto polls local at interval", watchSetup{local: two, files: map[string]string{defaultConf: "web_interval = 9"}}, []string{"--watch"}, time.Second},
		{"web default", watchSetup{web: two}, []string{"--watch", "--source", "web"}, 3 * time.Second},
		{"web config", watchSetup{web: two, files: map[string]string{defaultConf: "web_interval = 7"}}, []string{"--watch", "--source", "web"}, 7 * time.Second},
		{"web ignores local interval key", watchSetup{web: two, files: map[string]string{defaultConf: "interval = 2\nweb_interval = 7"}}, []string{"--watch", "--source", "web"}, 7 * time.Second},
		{"web flag", watchSetup{web: two}, []string{"--watch", "--source", "web", "--interval", "1.5"}, 1500 * time.Millisecond},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := runWatch(t, tc.set, tc.args...)
			if r.code != 0 {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if want := []time.Duration{tc.want, tc.want}; !slices.Equal(r.sleeps, want) {
				t.Errorf("sleeps = %v, want %v", r.sleeps, want)
			}
		})
	}
}

func TestWatchThrottlesWebInAuto(t *testing.T) {
	paused := playingOn(model.SourceMacOS, model.StatePaused)
	webPlaying := playingOn(model.SourceWeb, model.StatePlaying)
	local := make([]wstep, 4)
	web := make([]wstep, 4)
	for i := range local {
		local[i] = wstep{np: paused}
		web[i] = wstep{np: webPlaying}
	}
	r := runWatch(t, watchSetup{local: local, web: web}, "--watch")
	if r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	// Reads happen at t = 0, 1, 2 and 3 s; the 3 s web interval allows two.
	if r.local.calls != 4 || r.web.calls != 2 {
		t.Errorf("local reads %d, web reads %d; want 4 and 2", r.local.calls, r.web.calls)
	}
}

func TestWatchFatalErrors(t *testing.T) {
	tests := []struct {
		name string
		kind backend.Kind
		code int
	}{
		{"auth required", backend.KindAuthRequired, 5},
		{"permission denied", backend.KindPermissionDenied, 6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fatal := &backend.Error{Kind: tc.kind, Message: "no access", Hint: "do something"}
			r := runWatch(t, watchSetup{local: []wstep{{np: playing()}, {err: fatal}, {np: playing()}}}, "--watch")
			if r.code != tc.code {
				t.Errorf("exit = %d, want %d", r.code, tc.code)
			}
			if want := "spmeta: no access\nhint: do something\n"; r.stderr != want {
				t.Errorf("stderr = %q, want %q (printed exactly once)", r.stderr, want)
			}
			if r.stdout != playingText {
				t.Errorf("stdout = %q, want only the first item", r.stdout)
			}
		})
	}
}

func TestWatchKeepsRunningThroughTransientErrors(t *testing.T) {
	flaky := &backend.Error{Kind: backend.KindUnexpected, Message: "osascript timed out"}
	limited := &backend.Error{Kind: backend.KindRateLimited, Message: "rate limited", RetryAfter: 2 * time.Second}
	r := runWatch(t, watchSetup{local: []wstep{{err: flaky}, {err: flaky}, {err: limited}, {np: playing()}}}, "--watch")
	if r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if want := "spmeta: osascript timed out\n"; r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
	if r.stdout != playingText {
		t.Errorf("stdout = %q, want %q", r.stdout, playingText)
	}
	if want := []time.Duration{time.Second, time.Second, 2 * time.Second, time.Second}; !slices.Equal(r.sleeps, want) {
		t.Errorf("sleeps = %v, want %v", r.sleeps, want)
	}
}

func TestWatchUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"quiet with watch", []string{"--watch", "--quiet"}, "--quiet"},
		{"interval without watch", []string{"--interval", "2"}, "--watch"},
		{"interval below local minimum", []string{"--watch", "--interval", "0.4"}, "0.5"},
		{"interval below web minimum", []string{"--watch", "--source", "web", "--interval", "0.9"}, "1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := runWatch(t, watchSetup{local: []wstep{{np: playing()}}}, tc.args...)
			if r.code != 2 || r.stdout != "" || !strings.Contains(r.stderr, tc.want) || !strings.Contains(r.stderr, usageHint) {
				t.Errorf("exit %d, stdout %q, stderr %q; want a usage error mentioning %q", r.code, r.stdout, r.stderr, tc.want)
			}
			if r.local.calls != 0 {
				t.Errorf("backend read %d times before the usage error", r.local.calls)
			}
		})
	}
}

func TestWatchReadsChangeBeforeTheInterval(t *testing.T) {
	var notifierCtx context.Context
	r := runWatch(t, watchSetup{
		local: []wstep{{np: playing()}, titled("Second", model.StatePlaying)},
		changes: func(ctx context.Context) (<-chan struct{}, error) {
			notifierCtx = ctx
			wake := make(chan struct{}, 1)
			wake <- struct{}{} // the player signalled during the first read
			context.AfterFunc(ctx, func() { close(wake) })
			return wake, nil
		},
	}, "--watch", "--source", "local")
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if want := playingText + "▶ Second — Alvan · Home (2:00 / 3:58)\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	// The change is read after the minimum gap; then the next wait is the
	// gap plus the rest of the interval.
	gap := watch.MinWakeGap
	if want := []time.Duration{gap, gap, time.Second - gap}; !slices.Equal(r.sleeps, want) {
		t.Errorf("sleeps = %v, want %v: the change must be read after the gap, not the interval", r.sleeps, want)
	}
	if notifierCtx == nil || notifierCtx.Err() == nil {
		t.Error("the notifier must be stopped when watch returns")
	}
}

// main calls os.Exit as soon as Run returns, so a notifier still stopping,
// such as a listener process being killed and reaped, would be orphaned.
func TestWatchWaitsForTheNotifierToStop(t *testing.T) {
	var stopped atomic.Bool
	r := runWatch(t, watchSetup{
		local: []wstep{{np: playing()}},
		changes: func(ctx context.Context) (<-chan struct{}, error) {
			wake := make(chan struct{})
			go func() {
				<-ctx.Done()
				time.Sleep(50 * time.Millisecond) // stopping a listener takes a while
				stopped.Store(true)
				close(wake)
			}()
			return wake, nil
		},
	}, "--watch", "--source", "local")
	if r.code != 0 || r.stdout != playingText {
		t.Fatalf("exit %d, stdout %q; want 0 and one line", r.code, r.stdout)
	}
	if !stopped.Load() {
		t.Error("Run returned before the notifier closed its channel")
	}
}

func TestWatchDoesNotWaitForeverForTheNotifier(t *testing.T) {
	start := time.Now()
	r := runWatch(t, watchSetup{
		local: []wstep{{np: playing()}},
		changes: func(context.Context) (<-chan struct{}, error) {
			return make(chan struct{}), nil // never closes
		},
	}, "--watch", "--source", "local")
	if r.code != 0 || r.stdout != playingText {
		t.Fatalf("exit %d, stdout %q; want 0 and one line", r.code, r.stdout)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Run took %v; a notifier that never stops must not hold it past the stop bound", elapsed)
	}
}

func TestWatchPollsWhenNotificationsFail(t *testing.T) {
	failing := func(context.Context) (<-chan struct{}, error) {
		return nil, errors.New("no D-Bus session bus found")
	}
	tests := []struct {
		name      string
		args      []string
		wantDebug bool
	}{
		{"silent without debug", []string{"--watch"}, false},
		{"logged with debug", []string{"--watch", "--debug"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := runWatch(t, watchSetup{local: []wstep{{np: playing()}, {np: playing()}}, changes: failing}, tc.args...)
			if r.code != 0 || r.stdout != playingText {
				t.Fatalf("exit %d, stdout %q; want 0 and one line", r.code, r.stdout)
			}
			if want := []time.Duration{time.Second, time.Second}; !slices.Equal(r.sleeps, want) {
				t.Errorf("sleeps = %v, want %v", r.sleeps, want)
			}
			logged := strings.Contains(r.stderr, "change notifications unavailable") &&
				strings.Contains(r.stderr, "no D-Bus session bus found")
			if logged != tc.wantDebug {
				t.Errorf("stderr = %q; want the failure logged: %v", r.stderr, tc.wantDebug)
			}
			if !tc.wantDebug && r.stderr != "" {
				t.Errorf("stderr = %q, want nothing without --debug", r.stderr)
			}
		})
	}
}

func TestWatchStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &scriptedBackend{src: model.SourceMacOS, steps: []wstep{{np: playing()}}, cancel: cancel}
	var out, errOut bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- cli.Run(ctx, []string{"--watch", "--interval", "30"}, &out, &errOut, cli.BuildInfo{Version: "dev"},
			cli.WithBackend(b), cli.WithEnv(config.Env{}))
	}()
	time.AfterFunc(50*time.Millisecond, cancel)
	select {
	case code := <-done:
		if code != 0 || out.String() != playingText || errOut.Len() != 0 {
			t.Errorf("exit %d, stdout %q, stderr %q; want 0, one line, no stderr", code, out.String(), errOut.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not stop on cancellation")
	}
}
