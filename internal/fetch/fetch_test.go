package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xataru/jobster/internal/job"
)

type fakeFetcher struct {
	name string
	fn   func(ctx context.Context) ([]job.Job, error)
}

func (f fakeFetcher) Name() string                                 { return f.name }
func (f fakeFetcher) Fetch(ctx context.Context) ([]job.Job, error) { return f.fn(ctx) }

func TestRunIsolatesFailures(t *testing.T) {
	fetchers := []Fetcher{
		fakeFetcher{"ok", func(context.Context) ([]job.Job, error) {
			return []job.Job{{Title: "a"}, {Title: "b"}}, nil
		}},
		fakeFetcher{"broken", func(context.Context) ([]job.Job, error) {
			return nil, errors.New("boom")
		}},
		fakeFetcher{"panics", func(context.Context) ([]job.Job, error) {
			var m map[string]int
			m["x"]++
			return nil, nil
		}},
		fakeFetcher{"slow", func(ctx context.Context) ([]job.Job, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}},
	}

	results := Run(context.Background(), fetchers, 2, 50*time.Millisecond)

	if len(results) != len(fetchers) {
		t.Fatalf("got %d results, want %d", len(results), len(fetchers))
	}
	for i, r := range results {
		if r.Source != fetchers[i].Name() {
			t.Errorf("results[%d].Source = %q, want %q (order must be preserved)", i, r.Source, fetchers[i].Name())
		}
	}
	if results[0].Err != nil || len(results[0].Jobs) != 2 {
		t.Errorf("ok: err=%v jobs=%d", results[0].Err, len(results[0].Jobs))
	}
	if results[1].Err == nil {
		t.Error("broken: want error")
	}
	if results[2].Err == nil || !strings.Contains(results[2].Err.Error(), "panic") {
		t.Errorf("panics: err=%v, want recovered panic", results[2].Err)
	}
	if !errors.Is(results[3].Err, context.DeadlineExceeded) {
		t.Errorf("slow: err=%v, want deadline exceeded", results[3].Err)
	}
}

func TestGreenhouseFetch(t *testing.T) {
	fixture, err := os.ReadFile("testdata/greenhouse.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/boards/examplecloud/jobs" || r.URL.Query().Get("content") != "true" {
			http.NotFound(w, r)
			return
		}
		w.Write(fixture)
	}))
	defer srv.Close()

	g := &Greenhouse{Client: NewClient(), Board: "examplecloud", BaseURL: srv.URL}
	jobs, err := g.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 4 {
		t.Errorf("got %d jobs, want 4", len(jobs))
	}

	missing := &Greenhouse{Client: NewClient(), Board: "nope", BaseURL: srv.URL}
	if _, err := missing.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want 404", err)
	}
}

func TestRemoteOKRetriesServerErrors(t *testing.T) {
	retryDelay = time.Millisecond
	t.Cleanup(func() { retryDelay = 2 * time.Second })

	fixture, err := os.ReadFile("testdata/remoteok.json")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "jobster/") {
			t.Errorf("User-Agent = %q", ua)
		}
		w.Write(fixture)
	}))
	defer srv.Close()

	jobs, err := (&RemoteOK{Client: NewClient(), BaseURL: srv.URL}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 7 || calls.Load() != 2 {
		t.Errorf("jobs=%d calls=%d, want 7 jobs after 2 calls", len(jobs), calls.Load())
	}
}
