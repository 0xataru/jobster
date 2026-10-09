package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseDjinni(t *testing.T) {
	jobs, err := ParseDjinni(openFixture(t, "djinni.xml"), "djinni:golang")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(jobs))
	}
	j := jobs[0]
	if j.Source != "djinni:golang" || j.Title != "Senior Go Engineer" || j.Company != "" || j.Location != "" {
		t.Errorf("unexpected fields: %+v", j)
	}
	if j.URL != "https://djinni.co/jobs/900001-senior-go-engineer/" {
		t.Errorf("URL = %q", j.URL)
	}
	if !strings.Contains(j.Description, "Build Go microservices on Kubernetes.") || strings.Contains(j.Description, "<") {
		t.Errorf("Description = %q", j.Description)
	}
	if len(j.Tags) != 1 || j.Tags[0] != "Golang" {
		t.Errorf("Tags = %q, want empty categories dropped", j.Tags)
	}
	if want := time.Date(2026, 10, 9, 11, 25, 24, 0, time.UTC); !j.PostedAt.Equal(want) {
		t.Errorf("PostedAt = %v, want %v", j.PostedAt, want)
	}
	// Anonymous postings with the same title must not collapse into one.
	if jobs[0].Key() == jobs[1].Key() {
		t.Error("anonymous Djinni jobs with equal titles share a dedupe key")
	}
}

func TestDjinniFetchQuery(t *testing.T) {
	fixture, err := os.ReadFile("testdata/djinni.xml")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/jobs/rss/" || q.Get("primary_keyword") != "Golang" || q.Get("employment") != "remote" {
			t.Errorf("unexpected request %s", r.URL)
		}
		w.Write(fixture)
	}))
	defer srv.Close()

	d := &Djinni{Client: NewClient(), Keyword: "Golang", Params: map[string]string{"employment": "remote"}, BaseURL: srv.URL}
	if jobs, err := d.Fetch(context.Background()); err != nil || len(jobs) != 2 {
		t.Errorf("jobs=%d err=%v", len(jobs), err)
	}
}

func TestParseWeWorkRemotely(t *testing.T) {
	jobs, err := ParseWeWorkRemotely(openFixture(t, "wwr.rss"), "wwr:remote-back-end-programming-jobs")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(jobs))
	}
	j := jobs[0]
	if j.Company != "Ferrous Cloud" || j.Title != "Senior Rust Developer" {
		t.Errorf("Company/Title = %q / %q, want split on \": \"", j.Company, j.Title)
	}
	if j.Location != "Anywhere in the World; 🇪🇸 Spain, 🇩🇪 Germany" {
		t.Errorf("Location = %q", j.Location)
	}
	if strings.Join(j.Tags, "|") != "Rust|PostgreSQL|Kubernetes" {
		t.Errorf("Tags = %q", j.Tags)
	}
	if want := time.Date(2026, 10, 8, 10, 51, 23, 0, time.UTC); !j.PostedAt.Equal(want) {
		t.Errorf("PostedAt = %v", j.PostedAt)
	}
	if jobs[1].Company != "" || jobs[1].Title != "Untitled Posting Without Company" {
		t.Errorf("title without company: %+v", jobs[1])
	}
}

func TestParseRemotive(t *testing.T) {
	jobs, err := ParseRemotive(openFixture(t, "remotive.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 3 {
		t.Fatalf("got %d jobs, want 3", len(jobs))
	}
	j := jobs[0]
	if j.ExternalID != "2091001" || j.Company != "Gopher GmbH" || j.Location != "Europe" || j.Description != "Go & Kubernetes." {
		t.Errorf("unexpected fields: %+v", j)
	}
	if j.SalaryMin != 80000 || j.SalaryMax != 100000 || j.Currency != "EUR" {
		t.Errorf("salary = %d-%d %s", j.SalaryMin, j.SalaryMax, j.Currency)
	}
	if want := time.Date(2026, 10, 7, 1, 11, 9, 0, time.UTC); !j.PostedAt.Equal(want) {
		t.Errorf("PostedAt = %v", j.PostedAt)
	}
	if jobs[1].SalaryMax != 0 {
		t.Errorf("hourly rate read as salary: %d", jobs[1].SalaryMax)
	}
	if jobs[2].SalaryMin != 90000 || jobs[2].SalaryMax != 105000 {
		t.Errorf("\"$90-105k\" = %d-%d", jobs[2].SalaryMin, jobs[2].SalaryMax)
	}
}

func TestParseHimalayas(t *testing.T) {
	jobs, err := ParseHimalayas(openFixture(t, "himalayas.json"), "himalayas:golang")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(jobs))
	}
	j := jobs[0]
	if j.Company != "Orbit Labs" || j.Location != "Germany; Spain" || j.Description != "Distributed Go services." {
		t.Errorf("unexpected fields: %+v", j)
	}
	if j.SalaryMin != 100000 || j.SalaryMax != 140000 || j.Currency != "EUR" {
		t.Errorf("salary = %d-%d %s", j.SalaryMin, j.SalaryMax, j.Currency)
	}
	if strings.Join(j.Tags, "|") != "Senior|Golang-Developer|Backend-Engineer" {
		t.Errorf("Tags = %q", j.Tags)
	}
	if want := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC); !j.PostedAt.Equal(want) {
		t.Errorf("PostedAt = %v", j.PostedAt)
	}
	if jobs[1].Location != "Worldwide" || jobs[1].Currency != "" {
		t.Errorf("no restrictions: Location=%q Currency=%q", jobs[1].Location, jobs[1].Currency)
	}
}

func TestHimalayasPaging(t *testing.T) {
	var pages atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages.Add(1)
		q := r.URL.Query()
		if q.Get("q") != "golang" || q.Get("sort") != "recent" {
			t.Errorf("unexpected query %s", r.URL.RawQuery)
		}
		// A short first page must not end paging; the total (23) does.
		n := 16
		if q.Get("page") == "2" {
			n = 3
		}
		var b strings.Builder
		b.WriteString(`{"totalCount": 23, "jobs": [`)
		for i := range n {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`{"title": "Go Engineer", "companyName": "C` + q.Get("page") + `", "pubDate": 1791460800,
				"applicationLink": "https://himalayas.app/x", "locationRestrictions": []}`)
		}
		b.WriteString("]}")
		w.Write([]byte(b.String()))
	}))
	defer srv.Close()

	h := &Himalayas{Client: NewClient(), Query: "golang", Pages: 5, BaseURL: srv.URL}
	jobs, err := h.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 19 || pages.Load() != 2 {
		t.Errorf("jobs=%d pages=%d, want 19 jobs from 2 pages", len(jobs), pages.Load())
	}
}

func TestParseSalary(t *testing.T) {
	tests := []struct {
		in       string
		lo, hi   int
		currency string
		ok       bool
	}{
		{"$90k - $105k", 90000, 105000, "USD", true},
		{"$20k -$35k", 20000, 35000, "USD", true},
		{"€80,000–100,000", 80000, 100000, "EUR", true},
		{"£70.000 - £90.000", 70000, 90000, "GBP", true},
		{"$90-105k", 90000, 105000, "USD", true},
		{"120k", 120000, 120000, "", true},
		{"$170k - $200k + equity", 170000, 200000, "USD", true},
		{"$45-$120/Hour", 0, 0, "", false},
		{"$90 - $150 /hour", 0, 0, "", false},
		{"€5,000 per month", 0, 0, "", false},
		{"90 - 150", 0, 0, "", false},
		{"competitive", 0, 0, "", false},
		{"", 0, 0, "", false},
	}
	for _, tt := range tests {
		lo, hi, cur, ok := parseSalary(tt.in)
		if lo != tt.lo || hi != tt.hi || cur != tt.currency || ok != tt.ok {
			t.Errorf("parseSalary(%q) = %d, %d, %q, %v; want %d, %d, %q, %v",
				tt.in, lo, hi, cur, ok, tt.lo, tt.hi, tt.currency, tt.ok)
		}
	}
}

func TestDecodeRSSRejectsHTML(t *testing.T) {
	for _, parse := range []func() error{
		func() error {
			_, err := ParseDjinni(strings.NewReader("<html><body>blocked</body></html>"), "d")
			return err
		},
		func() error {
			_, err := ParseWeWorkRemotely(strings.NewReader("<!DOCTYPE html><html></html>"), "w")
			return err
		},
	} {
		if err := parse(); err == nil || !strings.Contains(err.Error(), "not an RSS feed") {
			t.Errorf("err = %v, want not-an-RSS-feed error", err)
		}
	}
}
