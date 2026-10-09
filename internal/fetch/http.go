package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	userAgent    = "jobster/0.1 (+https://github.com/0xataru/jobster)"
	maxBodyBytes = 64 << 20
)

// retryDelay is a variable so tests can shorten it.
var retryDelay = 2 * time.Second

// Client is the HTTP client shared by fetchers.
type Client struct {
	HTTP *http.Client
}

// NewClient returns a client; per-request deadlines come from the context.
func NewClient() *Client {
	return &Client{HTTP: &http.Client{}}
}

// getJSON GETs url and decodes the JSON body into v.
func (c *Client) getJSON(ctx context.Context, url string, v any) error {
	body, err := c.get(ctx, url)
	if err != nil {
		return err
	}
	defer body.Close()
	if err := json.NewDecoder(body).Decode(v); err != nil {
		return fmt.Errorf("GET %s: decode: %w", url, err)
	}
	return nil
}

// get GETs url and returns the body of a 200 response, size-limited,
// retrying once on throttling or server errors. The caller closes it.
func (c *Client) get(ctx context.Context, url string) (io.ReadCloser, error) {
	var err error
	for attempt := range 2 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryDelay):
			}
		}
		var body io.ReadCloser
		var retry bool
		body, retry, err = c.tryGet(ctx, url)
		if err == nil || !retry {
			return body, err
		}
	}
	return nil, err
}

func (c *Client) tryGet(ctx context.Context, url string) (body io.ReadCloser, retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, application/rss+xml, application/xml;q=0.9, */*;q=0.1")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, ctx.Err() == nil, fmt.Errorf("GET %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		retry = resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return nil, retry, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return limitedBody{io.LimitReader(resp.Body, maxBodyBytes), resp.Body, resp.Request.URL}, false, nil
}

// limitedBody is a size-limited response body that remembers the final URL
// after redirects.
type limitedBody struct {
	io.Reader
	io.Closer
	url *neturl.URL
}

var (
	blockTag = regexp.MustCompile(`(?i)<\s*(br|/p|/div|/li|/h[1-6]|li)\b[^>]*>`)
	anyTag   = regexp.MustCompile(`<[^>]*>`)
	blanks   = regexp.MustCompile(`[ \t\r\f\v\x{a0}]+`)
	newlines = regexp.MustCompile(`\s*\n\s*`)
	// Tags become spaces, which strands punctuation after inline markup:
	// "<b>Go</b>." would read "Go .".
	spaceBeforePunct = regexp.MustCompile(` +([.,;:!?)])`)
)

// htmlToText reduces an HTML fragment to readable plain text. It is meant for
// keyword matching and previews, not faithful rendering.
func htmlToText(s string) string {
	s = blockTag.ReplaceAllString(s, "\n")
	s = anyTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = blanks.ReplaceAllString(s, " ")
	s = newlines.ReplaceAllString(s, "\n")
	s = spaceBeforePunct.ReplaceAllString(s, "$1")
	return strings.TrimSpace(s)
}

// fixMojibake repairs UTF-8 text that was decoded as Latin-1 and re-encoded,
// e.g. "fÃ¼r" → "für". Strings that do not decode cleanly are returned as is.
func fixMojibake(s string) string {
	buf := make([]byte, 0, len(s))
	suspicious := false
	for _, r := range s {
		if r > 0xFF {
			return s
		}
		if r >= 0x80 {
			suspicious = true
		}
		buf = append(buf, byte(r))
	}
	if !suspicious || !utf8.Valid(buf) {
		return s
	}
	return string(buf)
}
