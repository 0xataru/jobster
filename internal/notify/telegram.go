package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xataru/jobster/internal/job"
)

const (
	// Telegram rejects messages over 4096 characters; leave headroom.
	maxMessageRunes = 3800
	maxTitleRunes   = 140
	maxLocRunes     = 90
	maxRetryAfter   = 30 * time.Second
)

// Telegram sends the digest through a Telegram bot.
type Telegram struct {
	Token   string
	ChatID  string
	HTTP    *http.Client
	BaseURL string // defaults to https://api.telegram.org
}

// Notify sends one message per chunk of jobs, sized to Telegram's limit.
// Nothing is sent for an empty digest.
func (t *Telegram) Notify(ctx context.Context, jobs []job.Scored) error {
	for i, msg := range telegramMessages(jobs) {
		if err := t.send(ctx, msg); err != nil {
			return fmt.Errorf("telegram: message %d: %w", i+1, err)
		}
	}
	return nil
}

// telegramMessages renders jobs as HTML messages, splitting between jobs so
// no message exceeds maxMessageRunes.
func telegramMessages(jobs []job.Scored) []string {
	if len(jobs) == 0 {
		return nil
	}
	noun := "jobs"
	if len(jobs) == 1 {
		noun = "job"
	}
	header := fmt.Sprintf("<b>🔎 %d new matching %s</b>\n", len(jobs), noun)

	var msgs []string
	cur := header
	for _, j := range jobs {
		entry := telegramEntry(j)
		if utf8.RuneCountInString(cur)+utf8.RuneCountInString(entry) > maxMessageRunes && cur != header {
			msgs = append(msgs, cur)
			cur = ""
		}
		cur += entry
	}
	return append(msgs, cur)
}

func telegramEntry(j job.Scored) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n<b>[%d]</b> <a href=\"%s\">%s</a>\n", j.Score,
		html.EscapeString(j.URL), html.EscapeString(truncate(j.Title, maxTitleRunes)))

	var meta []string
	if j.Company != "" {
		meta = append(meta, "<b>"+html.EscapeString(j.Company)+"</b>")
	}
	if j.Location != "" && j.Location != j.Title {
		meta = append(meta, html.EscapeString(truncate(j.Location, maxLocRunes)))
	}
	if s := Salary(j.Job); s != "" {
		meta = append(meta, "💰 "+html.EscapeString(s))
	}
	if !j.PostedAt.IsZero() {
		meta = append(meta, j.PostedAt.Format("Jan 2"))
	}
	meta = append(meta, "via "+html.EscapeString(j.Source))
	b.WriteString(strings.Join(meta, " · "))
	b.WriteString("\n")
	return b.String()
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

func (t *Telegram) send(ctx context.Context, text string) error {
	payload, err := json.Marshal(map[string]any{
		"chat_id":              t.ChatID,
		"text":                 text,
		"parse_mode":           "HTML",
		"link_preview_options": map[string]bool{"is_disabled": true},
	})
	if err != nil {
		return err
	}
	for attempt := range 2 {
		retryAfter, err := t.post(ctx, payload)
		if err == nil || retryAfter == 0 || attempt > 0 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryAfter):
		}
	}
	return nil
}

// post sends one request. On HTTP 429 it returns how long Telegram asked us
// to wait. Errors never include the request URL, which contains the token.
func (t *Telegram) post(ctx context.Context, payload []byte) (retryAfter time.Duration, err error) {
	base := t.BaseURL
	if base == "" {
		base = "https://api.telegram.org"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/bot"+t.Token+"/sendMessage", bytes.NewReader(payload))
	if err != nil {
		return 0, errors.New("build request") // the URL holds the token
	}
	req.Header.Set("Content-Type", "application/json")

	client := t.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return 0, fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Parameters  struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("%s: decode response: %w", resp.Status, err)
	}
	if body.OK {
		return 0, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter = min(time.Duration(max(body.Parameters.RetryAfter, 1))*time.Second, maxRetryAfter)
	}
	return retryAfter, fmt.Errorf("%s: %s", resp.Status, body.Description)
}
