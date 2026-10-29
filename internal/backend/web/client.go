package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

const (
	playerPath = "/v1/me/player?additional_types=episode"
	userAgent  = "spmeta/%s (+https://github.com/bkuberek/spmeta)"

	requestTimeout   = 5 * time.Second
	serverRetryDelay = time.Second
	maxRateAttempts  = 3
	defaultWait      = 5 * time.Second
	// maxWait caps a Retry-After so that the duration cannot overflow.
	maxWait = 24 * time.Hour

	maxResponseBody = 1 << 20
	// maxServerText is the longest error.message shown to the user.
	maxServerText = 200

	reasonQuota = "QUOTA_EXCEEDED"
	hintAllowed = "Your account may not be on the app's user allowlist in the Spotify Developer Dashboard, or the app owner's Premium subscription may have lapsed."
)

type response struct {
	status int
	header http.Header
	body   []byte
}

// attempts counts what one Read has used of each retry budget.
type attempts struct {
	refreshed     bool     // the 401 refresh is spent
	serverRetried bool     // the 5xx and network retry is spent
	limited       int      // 429 responses so far
	tokens        []string // every access token this Read sent, to keep out of messages
}

// outcome is what to do after one attempt: return body or err, or wait, maybe
// refresh the token, and try again.
type outcome struct {
	body    []byte
	err     error
	retry   bool
	refresh bool
	wait    time.Duration
}

// fetch performs the request and the retries of the response table. A 200
// returns its body; every other end is an error.
func (b *Backend) fetch(ctx context.Context) ([]byte, error) {
	token, err := b.token(ctx, false)
	if err != nil {
		return nil, err
	}
	tries := &attempts{tokens: []string{token}}
	client := b.client()
	for {
		res, err := b.get(ctx, client, token)
		var out outcome
		if err != nil {
			out = tries.networkFailure(ctx, err)
		} else {
			out = b.classify(res, tries)
		}
		if !out.retry {
			return out.body, out.err
		}
		if out.refresh {
			if token, err = b.token(ctx, true); err != nil {
				return nil, err
			}
			tries.tokens = append(tries.tokens, token)
		}
		if out.wait > 0 {
			if err := b.sleep(ctx, out.wait); err != nil {
				return nil, errCancelled(err)
			}
		}
	}
}

// client returns the HTTP client with redirects switched off, so that no
// request ever leaves the configured base URL.
func (b *Backend) client() *http.Client {
	c := http.DefaultClient
	if b.HTTPClient != nil {
		c = b.HTTPClient
	}
	noRedirects := *c
	noRedirects.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &noRedirects
}

// get sends one request with its own 5 s timeout and reads at most 1 MiB of
// the body. Only the method, host, path, status and duration are logged.
func (b *Backend) get(ctx context.Context, client *http.Client, token string) (response, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	target := strings.TrimRight(b.Endpoints.WithDefaults().API, "/") + playerPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
	if err != nil {
		return response{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf(userAgent, b.version()))

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return response{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	b.log().Debug("http", "method", req.Method, "host", req.URL.Host, "path", req.URL.Path,
		"status", resp.StatusCode, "duration", time.Since(start))

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return response{}, err
	}
	return response{status: resp.StatusCode, header: resp.Header, body: body}, nil
}

// classify applies the response table to one answer.
func (b *Backend) classify(res response, tries *attempts) outcome {
	switch {
	case res.status == http.StatusOK:
		return outcome{body: res.body}
	case res.status == http.StatusNoContent:
		return outcome{err: errNothingPlaying()}
	case res.status == http.StatusUnauthorized:
		if tries.refreshed {
			return outcome{err: &backend.Error{
				Kind:    backend.KindAuthRequired,
				Source:  model.SourceWeb,
				Message: "Spotify rejected the access token even after a refresh.",
				Hint:    hintLogin,
			}}
		}
		tries.refreshed = true
		return outcome{retry: true, refresh: true}
	case res.status == http.StatusForbidden:
		return outcome{err: forbidden(res.body, tries.tokens)}
	case res.status == http.StatusTooManyRequests:
		return b.rateLimited(res, tries)
	case res.status >= http.StatusInternalServerError:
		return tries.serverRetry(unexpectedStatus(res.status))
	default:
		return outcome{err: unexpectedStatus(res.status)}
	}
}

// networkFailure classifies a request that got no answer. A cancelled read is
// final; a timeout or a failed connection is retried once like a 5xx.
func (tries *attempts) networkFailure(ctx context.Context, err error) outcome {
	switch {
	case ctx.Err() != nil:
		return outcome{err: errCancelled(err)}
	case errors.Is(err, context.DeadlineExceeded):
		return tries.serverRetry(unexpected(fmt.Sprintf("Spotify did not answer within %d s.", int(requestTimeout.Seconds())), err))
	default:
		return tries.serverRetry(unexpected("could not reach the Spotify Web API.", err))
	}
}

// serverRetry retries once after 1 s; the second failure ends with fail.
func (tries *attempts) serverRetry(fail *backend.Error) outcome {
	if tries.serverRetried {
		return outcome{err: fail}
	}
	tries.serverRetried = true
	return outcome{retry: true, wait: serverRetryDelay}
}

// rateLimited waits out a short Retry-After and retries, up to three attempts
// in all; otherwise it reports KindRateLimited.
func (b *Backend) rateLimited(res response, tries *attempts) outcome {
	tries.limited++
	wait := retryAfter(res.header)
	if wait > b.MaxRetryWait || tries.limited >= maxRateAttempts {
		msg := fmt.Sprintf("rate limited by Spotify; retry after %d s", int64(wait.Seconds()))
		if apiReason(res.body) == reasonQuota {
			msg += " (" + reasonQuota + ")"
		}
		return outcome{err: &backend.Error{Kind: backend.KindRateLimited, Source: model.SourceWeb, Message: msg, RetryAfter: wait}}
	}
	return outcome{retry: true, wait: wait}
}

// retryAfter parses Retry-After as whole seconds, at least 1. A missing or
// invalid value gives 5 s, and a huge one is capped at 24 h.
func retryAfter(h http.Header) time.Duration {
	secs, err := strconv.ParseInt(h.Get("Retry-After"), 10, 64)
	if err != nil || secs < 1 {
		return defaultWait
	}
	if secs > int64(maxWait/time.Second) {
		return maxWait
	}
	return time.Duration(secs) * time.Second
}

// apiBody is the error body of the Web API. Where "reason" sits is not
// documented, so both places are read.
type apiBody struct {
	Reason string `json:"reason"`
	Error  struct {
		Message string `json:"message"`
		Reason  string `json:"reason"`
	} `json:"error"`
}

// parseAPIBody tolerates any body: an unparsable one yields empty fields.
func parseAPIBody(body []byte) apiBody {
	var b apiBody
	_ = json.Unmarshal(body, &b)
	return b
}

func apiReason(body []byte) string {
	b := parseAPIBody(body)
	if b.Reason != "" {
		return b.Reason
	}
	return b.Error.Reason
}

// forbidden maps a 403. The body's error.message is server text, so it is
// shown only when it is one short printable line that repeats no access token.
func forbidden(body []byte, tokens []string) *backend.Error {
	msg := "Spotify refused access (403)"
	if detail := safeText(parseAPIBody(body).Error.Message, tokens); detail != "" {
		msg += ": " + detail
	} else {
		msg += "."
	}
	return &backend.Error{Kind: backend.KindPermissionDenied, Source: model.SourceWeb, Message: msg, Hint: hintAllowed}
}

// safeText returns s when it is one printable line of at most 200 bytes that
// contains none of the secrets, and an empty string otherwise.
func safeText(s string, secrets []string) string {
	if len(s) > maxServerText || strings.IndexFunc(s, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0 {
		return ""
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(s, secret) {
			return ""
		}
	}
	return s
}

func unexpectedStatus(code int) *backend.Error {
	return unexpected(fmt.Sprintf("unexpected HTTP status %d from the Spotify Web API", code), nil)
}

func errCancelled(cause error) *backend.Error { return unexpected("read cancelled", cause) }
