package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/0xataru/jobster/internal/job"
)

func sample(i int) job.Scored {
	return job.Scored{
		Job: job.Job{
			Title:     fmt.Sprintf("Senior Rust Engineer <%d> & co", i),
			Company:   "Ferrous & Sons",
			Location:  "Remote; Europe",
			URL:       fmt.Sprintf("https://example.com/jobs?id=%d&ref=x", i),
			Source:    "ashby:ferrous",
			PostedAt:  time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC),
			SalaryMin: 90000, SalaryMax: 120000, Currency: "EUR",
		},
		Score: 12,
	}
}

func TestTelegramEntryEscapesHTML(t *testing.T) {
	got := telegramEntry(sample(1))
	for _, want := range []string{
		`<a href="https://example.com/jobs?id=1&amp;ref=x">Senior Rust Engineer &lt;1&gt; &amp; co</a>`,
		"<b>Ferrous &amp; Sons</b>",
		"💰 90k–120k EUR",
		"Oct 7",
		"via ashby:ferrous",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("entry missing %q:\n%s", want, got)
		}
	}
}

func TestTelegramEntryTruncatesLongFields(t *testing.T) {
	j := sample(1)
	j.Location = strings.Repeat("Paris, France; ", 40)
	j.Title = strings.Repeat("x", 500)
	got := telegramEntry(j)
	if strings.Count(got, "Paris") > 10 || strings.Contains(got, strings.Repeat("x", maxTitleRunes)) {
		t.Errorf("fields not truncated:\n%s", got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("missing ellipsis:\n%s", got)
	}
}

func TestTelegramMessagesSplit(t *testing.T) {
	if msgs := telegramMessages(nil); msgs != nil {
		t.Errorf("empty digest produced %d messages", len(msgs))
	}

	jobs := make([]job.Scored, 60)
	for i := range jobs {
		jobs[i] = sample(i)
	}
	msgs := telegramMessages(jobs)
	if len(msgs) < 2 {
		t.Fatalf("got %d messages, want a split", len(msgs))
	}
	total := 0
	for i, m := range msgs {
		if n := utf8.RuneCountInString(m); n > maxMessageRunes {
			t.Errorf("message %d has %d runes", i, n)
		}
		total += strings.Count(m, "<a href=")
	}
	if total != len(jobs) {
		t.Errorf("messages contain %d jobs, want %d", total, len(jobs))
	}
	if !strings.HasPrefix(msgs[0], "<b>🔎 60 new matching jobs</b>") {
		t.Errorf("header = %q", msgs[0][:40])
	}
}

func TestTelegramNotify(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/botSECRET/sendMessage" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["chat_id"] != "42" || body["parse_mode"] != "HTML" {
			t.Errorf("body = %v", body)
		}
		w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()

	tg := &Telegram{Token: "SECRET", ChatID: "42", BaseURL: srv.URL}
	if err := tg.Notify(context.Background(), []job.Scored{sample(1), sample(2)}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
	if err := tg.Notify(context.Background(), nil); err != nil || calls.Load() != 1 {
		t.Errorf("empty digest: err=%v calls=%d, want no request", err, calls.Load())
	}
}

func TestTelegramRetriesAfterRateLimit(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"ok":false,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`))
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	tg := &Telegram{Token: "T", ChatID: "1", BaseURL: srv.URL}
	if err := tg.Notify(context.Background(), []job.Scored{sample(1)}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", calls.Load())
	}
}

func TestTelegramErrorsHideToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"ok":false,"description":"Bad Request: chat not found"}`))
	}))
	tg := &Telegram{Token: "SECRET-TOKEN", ChatID: "1", BaseURL: srv.URL}

	err := tg.Notify(context.Background(), []job.Scored{sample(1)})
	if err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Errorf("err = %v, want Telegram's description", err)
	}

	srv.Close() // connection refused: net/http errors normally embed the URL
	err = tg.Notify(context.Background(), []job.Scored{sample(1)})
	if err == nil {
		t.Fatal("want error from closed server")
	}
	if strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Errorf("error leaks token: %v", err)
	}
}
