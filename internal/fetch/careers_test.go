package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseWorkable(t *testing.T) {
	jobs, err := ParseWorkable(openFixture(t, "workable.json"), "exampleatomic", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(jobs))
	}
	j := jobs[0]
	if j.Company != "Example Atomic" || j.Source != "workable:exampleatomic" || j.ExternalID != "AB12CD34EF" {
		t.Errorf("unexpected fields: %+v", j)
	}
	if want := "Remote; Madrid; Spain; Lisbon; Portugal"; j.Location != want {
		t.Errorf("Location = %q, want %q", j.Location, want)
	}
	if j.Description != "Embedded Rust on Linux." {
		t.Errorf("Description = %q", j.Description)
	}
	if want := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC); !j.PostedAt.Equal(want) {
		t.Errorf("PostedAt = %v", j.PostedAt)
	}
	if strings.HasPrefix(jobs[1].Location, "Remote") {
		t.Errorf("on-site job marked remote: %q", jobs[1].Location)
	}
}

func TestParseRecruitee(t *testing.T) {
	jobs, err := ParseRecruitee(openFixture(t, "recruitee.json"), "examplefeeds", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(jobs))
	}
	j := jobs[0]
	if j.Company != "Example Feeds" || j.URL != "https://jobs.example.com/o/senior-go-developer" {
		t.Errorf("unexpected fields: %+v", j)
	}
	if want := "Remote; Utrecht, Utrecht, Netherlands; Netherlands"; j.Location != want {
		t.Errorf("Location = %q, want %q", j.Location, want)
	}
	if !strings.Contains(j.Description, "Go & Kafka") || !strings.Contains(j.Description, "5+ years of Go") {
		t.Errorf("Description = %q, want description and requirements", j.Description)
	}
	if j.SalaryMin != 70000 || j.SalaryMax != 90000 || j.Currency != "EUR" {
		t.Errorf("salary = %d-%d %s (min is a string in the API)", j.SalaryMin, j.SalaryMax, j.Currency)
	}
	if want := time.Date(2026, 10, 8, 8, 3, 1, 0, time.UTC); !j.PostedAt.Equal(want) {
		t.Errorf("PostedAt = %v", j.PostedAt)
	}
	if jobs[1].SalaryMax != 0 {
		t.Errorf("monthly salary read as annual: %d", jobs[1].SalaryMax)
	}
}

func TestParsePersonio(t *testing.T) {
	jobs, err := ParsePersonio(openFixture(t, "personio.xml"), "example", "", "https://example.jobs.personio.de")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1", len(jobs))
	}
	j := jobs[0]
	if j.Company != "Example GmbH" || j.Title != "Staff Software Engineer (Go)" || j.URL != "https://example.jobs.personio.de/job/1834171" {
		t.Errorf("unexpected fields: %+v", j)
	}
	if j.Location != "Munich; Berlin; Remote" {
		t.Errorf("Location = %q", j.Location)
	}
	if !strings.Contains(j.Description, "Your mission") || !strings.Contains(j.Description, "Build Go services on Kubernetes.") {
		t.Errorf("Description = %q", j.Description)
	}
	if _, err := ParsePersonio(strings.NewReader("<!DOCTYPE html><html></html>"), "x", "", ""); err == nil {
		t.Error("HTML page accepted as a Personio feed")
	}
}

func TestDetectATS(t *testing.T) {
	page := func(s string) string { return "<html><body>" + s + "</body></html>" }
	careers, _ := url.Parse("https://jobs.example.com/")
	tests := []struct {
		name, page string
		want       string // ATS.String(), "" for none
		pinnable   bool
	}{
		{"none", page(`<a href="/about">About</a>`), "", false},
		{"greenhouse embed", page(`<script src="https://boards.greenhouse.io/embed/job_board/js?for=acme"></script>`), "greenhouse:acme", true},
		{"greenhouse job link", page(`<a href="https://job-boards.greenhouse.io/acme/jobs/123">Job</a>`), "greenhouse:acme", true},
		{"ashby", page(`<a href="https://jobs.ashbyhq.com/Acme/abc">Job</a>`), "ashby:Acme", true},
		{"lever eu", page(`<a href="https://jobs.eu.lever.co/acme/abc">Job</a>`), "lever:acme", false},
		{"workable", page(`<a href="https://apply.workable.com/acme/j/ABC/">Job</a>`), "workable:acme", true},
		{"personio com", page(`<a href="https://acme.jobs.personio.com/job/1">Job</a>`), "personio:acme", false},
		{"teamtailor ignores api host", page(`<img src="https://api.teamtailor.com/x"><a href="https://acme.teamtailor.com/jobs">Jobs</a>`), "teamtailor:acme", true},
		{"recruitee analytics host ignored", page(`<script src="https://careers-analytics.recruitee.com/t.js"></script>`), "", false},
		{"recruitee on own domain", page(`<img src="https://careers.recruiteecdn.com/image/upload/x.png">`), "recruitee:jobs.example.com", false},
		{"most linked wins", page(`<a href="https://jobs.lever.co/acme/1">1</a><a href="https://jobs.lever.co/acme/2">2</a>
			<a href="https://boards.greenhouse.io/partner/jobs/1">partner</a>`), "lever:acme", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DetectATS(tt.page, careers)
			if tt.want == "" {
				if ok {
					t.Errorf("detected %s, want none", got)
				}
				return
			}
			if !ok || got.String() != tt.want {
				t.Errorf("DetectATS = %s (ok=%v), want %s", got, ok, tt.want)
			}
			if got.Pinnable() != tt.pinnable {
				t.Errorf("Pinnable = %v, want %v", got.Pinnable(), tt.pinnable)
			}
		})
	}
}

func TestJobLinks(t *testing.T) {
	careers, _ := url.Parse("https://acme.io/careers")
	doc := `<html><body><nav><a href="/careers">Careers</a></nav>
		<a href="/careers/7928089">Senior Backend Engineer</a>
		<a href="/careers/7928089#apply">Apply</a>
		<a href="https://acme.io/careers/7952094">Product Lead</a>
		<a href="/jobs/platform-engineer">Platform Engineer</a>
		<a href="/blog/hello">Blog</a>
		<a href="/careers/_payload.json">data</a>
		<a href="https://gitlab.com/acme/-/blob/main/careers/jobs.yml">Edit this page</a>
		<a href="https://other.com/careers/1">Elsewhere</a>
		<a href="mailto:jobs@acme.io">Mail</a>
		</body></html>`
	c := &Careers{Company: "Acme", URL: careers.String()}
	var got []string
	for _, l := range c.jobLinks(parsePage(doc, careers), careers) {
		got = append(got, l.URL.Path)
	}
	if want := "/careers/7928089 /careers/7952094 /jobs/platform-engineer"; strings.Join(got, " ") != want {
		t.Errorf("links = %q, want %q", got, want)
	}

	c.LinkPattern = regexp.MustCompile(`^/jobs/`)
	got = nil
	for _, l := range c.jobLinks(parsePage(doc, careers), careers) {
		got = append(got, l.URL.Path)
	}
	if strings.Join(got, " ") != "/jobs/platform-engineer" {
		t.Errorf("with link_pattern: %q", got)
	}
}

func TestStripCompanySuffix(t *testing.T) {
	for in, want := range map[string]string{
		"Senior Backend Engineer | Acme":       "Senior Backend Engineer",
		"Backend Engineer - Acme Inc":          "Backend Engineer",
		"Go Engineer at Acme":                  "Go Engineer",
		"Senior Backend Engineer (Go/Rust)":    "Senior Backend Engineer (Go/Rust)",
		"Engineer - Platform | Careers | Acme": "Engineer - Platform | Careers",
	} {
		if got := stripCompanySuffix(in, "Acme"); got != want {
			t.Errorf("stripCompanySuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeCareersSite serves a careers page linking to job pages, one of which
// carries schema.org JobPosting data and one of which is broken.
func fakeCareersSite(t *testing.T, careersPage string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/careers", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(careersPage))
	})
	mux.HandleFunc("/careers/1", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`<html><head><title>Senior Backend Engineer (Go/Rust) | Acme</title>
			<meta property="og:title" content="Senior Backend Engineer (Go/Rust)"></head>
			<body><header>Menu Go to dashboard</header><main><h1>Senior Backend Engineer</h1>
			<p>We are a remote-first team in Europe building Postgres tooling in Go and Rust.</p></main>
			<footer>© Acme</footer></body></html>`))
	})
	mux.HandleFunc("/careers/2", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`<html><head><title>Platform Engineer - Acme</title>
			<script type="application/ld+json">{"@context": "https://schema.org", "@graph": [
				{"@type": "Organization", "name": "Acme"},
				{"@type": "JobPosting", "title": "Platform Engineer", "datePosted": "2026-10-05",
				 "description": "<p>Kubernetes and <b>Go</b>.</p>", "jobLocationType": "TELECOMMUTE",
				 "applicantLocationRequirements": [{"@type": "Country", "name": "Spain"}, {"@type": "Country", "name": "Portugal"}],
				 "jobLocation": {"@type": "Place", "address": {"@type": "PostalAddress", "addressLocality": "Lisbon", "addressCountry": "PT"}},
				 "baseSalary": {"@type": "MonetaryAmount", "currency": "EUR",
				   "value": {"@type": "QuantitativeValue", "minValue": 90000, "maxValue": 120000, "unitText": "YEAR"}}}
			]}</script></head><body>Platform Engineer</body></html>`))
	})
	mux.HandleFunc("/careers/3", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "gone", http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestCareersReadsJobPages(t *testing.T) {
	srv, _ := fakeCareersSite(t, `<html><body>
		<a href="/careers/1">Senior Backend Engineer</a>
		<a href="/careers/2">Platform Engineer</a>
		<a href="/careers/3">Removed role</a></body></html>`)

	c := &Careers{Client: NewClient(), Company: "Acme", URL: srv.URL + "/careers"}
	jobs, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2 (the broken page skipped): %+v", len(jobs), jobs)
	}

	j := jobs[0]
	if j.Title != "Senior Backend Engineer (Go/Rust)" || j.Company != "Acme" || j.Source != "careers:acme" {
		t.Errorf("page job: %+v", j)
	}
	if j.URL != srv.URL+"/careers/1" || j.Location != "" || !j.PostedAt.IsZero() {
		t.Errorf("page job URL/Location/PostedAt: %q %q %v", j.URL, j.Location, j.PostedAt)
	}
	if !strings.Contains(j.Description, "Postgres tooling in Go and Rust") {
		t.Errorf("Description = %q", j.Description)
	}
	if strings.Contains(j.Description, "Go to dashboard") || strings.Contains(j.Description, "©") {
		t.Errorf("header/footer text leaked into Description: %q", j.Description)
	}

	p := jobs[1]
	if p.Title != "Platform Engineer" || p.Description != "Kubernetes and Go." {
		t.Errorf("JSON-LD job: %+v", p)
	}
	if want := "Remote; Lisbon; Portugal; Spain"; p.Location != want { // PT expanded, then deduplicated
		t.Errorf("JSON-LD Location = %q, want %q", p.Location, want)
	}
	if p.SalaryMin != 90000 || p.SalaryMax != 120000 || p.Currency != "EUR" {
		t.Errorf("JSON-LD salary = %d-%d %s", p.SalaryMin, p.SalaryMax, p.Currency)
	}
	if want := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC); !p.PostedAt.Equal(want) {
		t.Errorf("JSON-LD PostedAt = %v", p.PostedAt)
	}
}

func TestCareersUsesEmbeddedPostings(t *testing.T) {
	srv, hits := fakeCareersSite(t, `<html><head><script type="application/ld+json">[
		{"@type": "JobPosting", "title": "Rust Engineer", "url": "https://acme.io/careers/9", "description": "Rust"},
		{"@type": ["JobPosting"], "title": "Go Engineer", "description": "Go"}
	]</script></head><body><a href="/careers/1">ignored</a></body></html>`)

	c := &Careers{Client: NewClient(), Company: "Acme", URL: srv.URL + "/careers"}
	jobs, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 || jobs[0].URL != "https://acme.io/careers/9" || jobs[1].URL != srv.URL+"/careers" {
		t.Errorf("jobs = %+v", jobs)
	}
	if hits.Load() != 1 {
		t.Errorf("fetched %d pages, want only the careers page", hits.Load())
	}
}

func TestCareersUsesRecruiteeOnOwnDomain(t *testing.T) {
	offers, err := os.ReadFile("testdata/recruitee.json")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><img src="https://careers.recruiteecdn.com/image/upload/logo.png">
			<a href="/o/senior-go-developer">Senior Go Developer</a></body></html>`))
	})
	mux.HandleFunc("/api/offers/", func(w http.ResponseWriter, r *http.Request) { w.Write(offers) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Careers{Client: NewClient(), Company: "Example Feeds", URL: srv.URL + "/"}
	jobs, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 || !strings.HasPrefix(jobs[0].Source, "recruitee:") || jobs[0].SalaryMax != 90000 {
		t.Errorf("want the offers API's 2 jobs, got %+v", jobs)
	}
}

func TestCareersNoLinks(t *testing.T) {
	srv, _ := fakeCareersSite(t, `<html><body><div id="root"></div><script src="/app.js"></script></body></html>`)
	c := &Careers{Client: NewClient(), Company: "Acme", URL: srv.URL + "/careers"}
	if _, err := c.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "no job links") {
		t.Errorf("err = %v, want a no-job-links error", err)
	}
	d, err := c.Discover(context.Background())
	if err != nil || d.HasATS || d.Links != 0 || d.Postings != 0 {
		t.Errorf("Discover = %+v, %v", d, err)
	}
}

// A landing page that links to a separate openings page, whose job pages
// all carry the site's generic title (as ScyllaDB's do).
func TestCareersFollowsOpeningsAndFixesGenericTitles(t *testing.T) {
	jobPage := `<html><head><title>Job Openings | Acme</title><meta property="og:title" content="Job Openings"></head>
		<body><h1>Find Open Positions</h1><p>We use Rust.</p>
		<script type="application/ld+json">{"@type": "JobPosting", "title": "broken "quote"}</script></body></html>`
	mux := http.NewServeMux()
	mux.HandleFunc("/company/careers/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><a href="/company/careers/life">Life at Acme</a>
			<a href="/company/careers/job-openings/">Browse Open Positions</a></body></html>`))
	})
	mux.HandleFunc("/company/careers/job-openings/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>
			<a href="/company/careers/job-openings/co/engineering/all">Engineering</a>
			<a href="/company/careers/job-openings/co/poland/A1.B2/rust-engineer/all"><style>.x{color:red}</style>Rust Engineer</a>
			<a href="/company/careers/job-openings/co/spain/C3.D4/go-engineer/all">Go Engineer</a></body></html>`))
	})
	mux.HandleFunc("/company/careers/job-openings/co/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(jobPage))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Careers{Client: NewClient(), Company: "Acme", URL: srv.URL + "/company/careers/",
		LinkPattern: regexp.MustCompile(`/job-openings/co/[^/]+/[0-9A-F]{2}\.[^/]+/`)}

	d, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(d.Followed, "/company/careers/job-openings/") || d.Links != 2 {
		t.Errorf("Discover = %+v, want the openings page followed and 2 job links", d)
	}

	jobs, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, j := range jobs {
		titles = append(titles, j.Title)
	}
	if strings.Join(titles, "|") != "Rust Engineer|Go Engineer" {
		t.Errorf("titles = %q, want link texts instead of the generic page title (and no CSS)", titles)
	}
}

func TestCountryName(t *testing.T) {
	for in, want := range map[string]string{"PL": "Poland", "gb": "United Kingdom", "Spain": "Spain", "XX": "XX", "": ""} {
		if got := countryName(in); got != want {
			t.Errorf("countryName(%q) = %q, want %q", in, got, want)
		}
	}
}
