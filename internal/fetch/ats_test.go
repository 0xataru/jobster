package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestParseAshby(t *testing.T) {
	jobs, err := ParseAshby(openFixture(t, "ashby.json"), "examplerust", "Example Rust")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2 (unlisted skipped)", len(jobs))
	}
	j := jobs[0]
	if j.Source != "ashby:examplerust" || j.Company != "Example Rust" || j.ExternalID != "a1" {
		t.Errorf("unexpected fields: %+v", j)
	}
	if want := "Remote; Remote - Europe; Germany; Barcelona; Spain"; j.Location != want {
		t.Errorf("Location = %q, want %q", j.Location, want)
	}
	if j.SalaryMin != 95000 || j.SalaryMax != 125000 || j.Currency != "EUR" {
		t.Errorf("salary = %d-%d %s, want the annual Salary component", j.SalaryMin, j.SalaryMax, j.Currency)
	}
	if want := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC); !j.PostedAt.Equal(want) {
		t.Errorf("PostedAt = %v", j.PostedAt)
	}
	if got := jobs[1].Location; got != "On-site; New York; USA" {
		t.Errorf("Location = %q, want workplace type first", got)
	}
}

func TestParseLever(t *testing.T) {
	jobs, err := ParseLever(openFixture(t, "lever.json"), "examplego", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(jobs))
	}
	j := jobs[0]
	if j.Company != "examplego" || j.URL != "https://jobs.lever.co/examplego/l1" {
		t.Errorf("unexpected fields: %+v", j)
	}
	if want := "Remote; Lisbon; Remote - EU; PT"; j.Location != want {
		t.Errorf("Location = %q, want %q", j.Location, want)
	}
	for _, s := range []string{"Go microservices", "What you'll do", "Run Kubernetes", "gRPC"} {
		if !strings.Contains(j.Description, s) {
			t.Errorf("Description missing %q: %q", s, j.Description)
		}
	}
	if j.SalaryMax != 140000 || j.Currency != "EUR" {
		t.Errorf("salary = %d %s", j.SalaryMax, j.Currency)
	}
	if want := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC); !j.PostedAt.Equal(want) {
		t.Errorf("PostedAt = %v, want %v", j.PostedAt, want)
	}
	if jobs[1].SalaryMax != 0 {
		t.Errorf("hourly wage must not be read as an annual salary: %d", jobs[1].SalaryMax)
	}
}

func TestParseTeamtailor(t *testing.T) {
	jobs, err := ParseTeamtailor(openFixture(t, "teamtailor.rss"), "examplenordic", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(jobs))
	}
	j := jobs[0]
	if j.Company != "Example Nordic" || j.ExternalID != "tt-101" || j.Source != "teamtailor:examplenordic" {
		t.Errorf("unexpected fields: %+v", j)
	}
	if want := "Remote; Stockholm HQ; Stockholm; Sweden"; j.Location != want {
		t.Errorf("Location = %q, want %q", j.Location, want)
	}
	if j.Description != "Work on our Rust & Go backend." {
		t.Errorf("Description = %q", j.Description)
	}
	if want := time.Date(2026, 10, 5, 7, 5, 16, 0, time.UTC); !j.PostedAt.Equal(want) {
		t.Errorf("PostedAt = %v", j.PostedAt)
	}
	if jobs[1].Location != "On-site" {
		t.Errorf("Location = %q, want On-site", jobs[1].Location)
	}
}

func TestTeamtailorCustomDomainFailsClearly(t *testing.T) {
	_, err := ParseTeamtailor(strings.NewReader("<!DOCTYPE html><html><head><title>Careers</title></head></html>"), "x", "")
	if err == nil || !strings.Contains(err.Error(), "custom domain") {
		t.Errorf("err = %v, want a hint about custom domains", err)
	}
}

func TestParseHackerNews(t *testing.T) {
	jobs, err := ParseHackerNews(openFixture(t, "hn.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2 (replies, meta and deleted comments skipped): %+v", len(jobs), jobs)
	}
	j := jobs[0]
	if j.Company != "Ferrous Labs" {
		t.Errorf("Company = %q", j.Company)
	}
	if want := "Ferrous Labs | Senior Rust Engineer | REMOTE (EU) | Full-time | €90k–120k"; j.Title != want || j.Location != want {
		t.Errorf("Title/Location = %q / %q, want header %q", j.Title, j.Location, want)
	}
	if j.URL != "https://news.ycombinator.com/item?id=1001" {
		t.Errorf("URL = %q", j.URL)
	}
	if !strings.Contains(j.Description, "distributed database in Rust") || !strings.Contains(j.Description, "https://example.com/jobs") {
		t.Errorf("Description = %q", j.Description)
	}
	if got := jobs[1].Company; got != "Gopher Co" {
		t.Errorf("Company = %q, want URL stripped and em dash split", got)
	}
}

func TestHackerNewsFetchPicksHiringThread(t *testing.T) {
	thread, err := os.ReadFile("testdata/hn.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/search_by_date":
			w.Write([]byte(`{"hits": [
				{"objectID": "49922568", "title": "Ask HN: Who wants to be hired? (October 2026)"},
				{"objectID": "49922569", "title": "Ask HN: Who is hiring? (October 2026)"}]}`))
		case "/api/v1/items/49922569":
			w.Write(thread)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	jobs, err := (&HackerNews{Client: NewClient(), BaseURL: srv.URL}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Errorf("got %d jobs, want 2", len(jobs))
	}
}

func TestATSFetchersHitExpectedPaths(t *testing.T) {
	fixtures := map[string]string{
		"/posting-api/job-board/examplerust": "ashby.json",
		"/v0/postings/examplego":             "lever.json",
		"/jobs.rss":                          "teamtailor.rss",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := fixtures[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		data, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Error(err)
		}
		w.Write(data)
	}))
	defer srv.Close()

	c := NewClient()
	for _, f := range []Fetcher{
		&Ashby{Client: c, Board: "examplerust", BaseURL: srv.URL},
		&Lever{Client: c, Board: "examplego", BaseURL: srv.URL},
		&Teamtailor{Client: c, Board: "examplenordic", BaseURL: srv.URL},
	} {
		jobs, err := f.Fetch(context.Background())
		if err != nil || len(jobs) == 0 {
			t.Errorf("%s: jobs=%d err=%v", f.Name(), len(jobs), err)
		}
		for _, j := range jobs {
			if !strings.HasPrefix(j.Source, f.Name()) {
				t.Errorf("%s: Source = %q", f.Name(), j.Source)
			}
		}
	}
}
