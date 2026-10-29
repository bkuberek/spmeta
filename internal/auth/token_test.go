package auth_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bkuberek/spmeta/internal/auth"
	"github.com/bkuberek/spmeta/internal/backend"
)

const (
	storedAccess  = "AQD-stored-access-token"
	storedRefresh = "AQD-stored-refresh-token"
)

func sampleToken() auth.Token {
	return auth.Token{
		ClientID:     "0123456789abcdef0123456789abcdef",
		AccessToken:  storedAccess,
		TokenType:    "Bearer",
		RefreshToken: storedRefresh,
		Scope:        "user-read-playback-state",
		ExpiresAt:    time.Date(2026, 10, 27, 18, 40, 0, 0, time.UTC),
		AuthorizedAt: time.Date(2026, 10, 27, 17, 40, 0, 0, time.UTC),
	}
}

func skipWithoutUnixModes(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not enforced on Windows")
	}
}

func wantKind(t *testing.T, err error, kind backend.Kind) {
	t.Helper()
	_ = asBackend(t, err, kind)
}

// asBackend returns err as a *backend.Error of the given kind, or fails.
func asBackend(t *testing.T, err error, kind backend.Kind) *backend.Error {
	t.Helper()
	var be *backend.Error
	if !errors.As(err, &be) || be.Kind != kind {
		t.Fatalf("err = %v (%T), want a *backend.Error of kind %d", err, err, kind)
	}
	return be
}

func TestReadTokenFixture(t *testing.T) {
	t.Parallel()
	got, err := auth.ReadToken(filepath.Join("testdata", "token.json"))
	if err != nil {
		t.Fatalf("ReadToken = %v", err)
	}
	want := sampleToken()
	want.Version = 1
	if got != want {
		t.Errorf("ReadToken = %+v, want %+v", got, want)
	}
}

func TestWriteTokenMatchesFixtureFormat(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "token.json")
	if err := auth.WriteToken(path, sampleToken()); err != nil {
		t.Fatalf("WriteToken = %v", err)
	}
	got, _ := os.ReadFile(path)
	want, _ := os.ReadFile(filepath.Join("testdata", "token.json"))
	if !bytes.Equal(got, want) {
		t.Errorf("token file =\n%s\nwant\n%s", got, want)
	}
}

func TestWriteTokenNormalizesTimesToUTCSeconds(t *testing.T) {
	t.Parallel()
	tok := sampleToken()
	zone := time.FixedZone("UTC+2", 2*3600)
	tok.ExpiresAt = time.Date(2026, 10, 27, 20, 40, 0, 123456789, zone)
	path := filepath.Join(t.TempDir(), "token.json")
	if err := auth.WriteToken(path, tok); err != nil {
		t.Fatalf("WriteToken = %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"expires_at": "2026-10-27T18:40:00Z"`) {
		t.Errorf("expires_at not normalized in:\n%s", data)
	}
}

func TestWriteTokenCreatesPrivateDirectoryAndFile(t *testing.T) {
	t.Parallel()
	skipWithoutUnixModes(t)
	dir := filepath.Join(t.TempDir(), "spmeta")
	path := filepath.Join(dir, "token.json")
	if err := auth.WriteToken(path, sampleToken()); err != nil {
		t.Fatalf("WriteToken = %v", err)
	}
	di, err := os.Stat(dir)
	if err != nil || di.Mode().Perm() != 0o700 {
		t.Errorf("directory = %v, %v; want mode 0700", di, err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("token file = %v, %v; want mode 0600", fi, err)
	}
}

func TestWriteTokenReplacesAtomically(t *testing.T) {
	t.Parallel()
	skipWithoutUnixModes(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")
	if err := auth.WriteToken(path, sampleToken()); err != nil {
		t.Fatalf("first WriteToken = %v", err)
	}
	old, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = old.Close() }()

	next := sampleToken()
	next.AccessToken = "BQD-second-access-token"
	if err := auth.WriteToken(path, next); err != nil {
		t.Fatalf("second WriteToken = %v", err)
	}

	// A reader that opened the file before the write keeps seeing the old
	// contents: the file was replaced by rename, not rewritten in place.
	oldData, _ := io.ReadAll(old)
	if !strings.Contains(string(oldData), storedAccess) || strings.Contains(string(oldData), "second") {
		t.Errorf("open handle sees %q; want the old contents", oldData)
	}
	got, err := auth.ReadToken(path)
	if err != nil || got.AccessToken != next.AccessToken {
		t.Errorf("ReadToken = %+v, %v; want the new token", got, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries after the write, want only token.json", len(entries))
	}
}

func TestWriteTokenTightensWideMode(t *testing.T) {
	t.Parallel()
	skipWithoutUnixModes(t)
	path := filepath.Join(t.TempDir(), "token.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := auth.WriteToken(path, sampleToken()); err != nil {
		t.Fatalf("WriteToken = %v", err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestWriteTokenFailureLeavesNoTemporaryFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A non-empty directory in the way makes the final rename fail.
	blocker := filepath.Join(dir, "token.json")
	if err := os.MkdirAll(filepath.Join(blocker, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := auth.WriteToken(blocker, sampleToken())
	wantKind(t, err, backend.KindUnexpected)
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "token.json" {
		t.Errorf("directory entries = %v, want only token.json", entries)
	}
}

func TestWriteTokenWithoutPath(t *testing.T) {
	t.Parallel()
	wantKind(t, auth.WriteToken("", sampleToken()), backend.KindUnexpected)
}

func TestReadTokenMissingFileIsNotAvailable(t *testing.T) {
	t.Parallel()
	_, err := auth.ReadToken(filepath.Join(t.TempDir(), "token.json"))
	be := asBackend(t, err, backend.KindNotAvailable)
	if !strings.Contains(be.Hint, "spmeta auth login") {
		t.Errorf("Hint = %q, want the login command", be.Hint)
	}
	wantKind(t, errorOf(auth.ReadToken("")), backend.KindNotAvailable)
}

func errorOf[T any](_ T, err error) error { return err }

func TestReadTokenUnusableFileIsAuthRequired(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, content string
	}{
		{"empty file", ""},
		{"not JSON", "<html>"},
		{"truncated", `{"version": 1, "client_id": "abc"`},
		{"unknown version", `{"version": 2, "client_id": "c", "access_token": "a", "refresh_token": "r"}`},
		{"missing version", `{"client_id": "c", "access_token": "a", "refresh_token": "r"}`},
		{"missing access token", `{"version": 1, "client_id": "c", "refresh_token": "r"}`},
		{"missing refresh token", `{"version": 1, "client_id": "c", "access_token": "a"}`},
		{"missing client id", `{"version": 1, "access_token": "a", "refresh_token": "r"}`},
		{"bad time", `{"version": 1, "client_id": "c", "access_token": "a", "refresh_token": "r", "expires_at": "soon"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "token.json")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			be := asBackend(t, errorOf(auth.ReadToken(path)), backend.KindAuthRequired)
			if !strings.Contains(be.Hint, "spmeta auth login") {
				t.Errorf("Hint = %q, want the login command", be.Hint)
			}
		})
	}
}

func TestReadTokenUnreadableFileIsAuthRequired(t *testing.T) {
	t.Parallel()
	// A directory exists at the path but cannot be read as a file.
	dir := t.TempDir()
	wantKind(t, errorOf(auth.ReadToken(dir)), backend.KindAuthRequired)
}

func TestReadTokenAcceptsWideMode(t *testing.T) {
	t.Parallel()
	skipWithoutUnixModes(t)
	path := filepath.Join(t.TempDir(), "token.json")
	if err := auth.WriteToken(path, sampleToken()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := auth.ReadToken(path); err != nil || got.AccessToken != storedAccess {
		t.Errorf("ReadToken = %+v, %v; a wide mode must not stop the token being used", got, err)
	}
}

func TestTokenFormattingHidesSecrets(t *testing.T) {
	t.Parallel()
	tok := sampleToken()
	var logged bytes.Buffer
	slog.New(slog.NewTextHandler(&logged, nil)).Info("token", "token", tok)

	outputs := map[string]string{
		"%v":   fmt.Sprintf("%v", tok),
		"%+v":  fmt.Sprintf("%+v", tok),
		"%#v":  fmt.Sprintf("%#v", tok),
		"slog": logged.String(),
	}
	for name, out := range outputs {
		if strings.Contains(out, storedAccess) || strings.Contains(out, storedRefresh) {
			t.Errorf("%s output leaks a token value: %s", name, out)
		}
	}
}
