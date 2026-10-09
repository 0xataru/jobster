package filter

import (
	"os"
	"testing"
	"time"

	"github.com/0xataru/jobster/internal/config"
	"github.com/0xataru/jobster/internal/fetch"
	"github.com/0xataru/jobster/internal/job"
)

var now = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func exampleFilter(t *testing.T) *Filter {
	t.Helper()
	cfg, err := config.Load("../../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f, err := New(cfg.Filter)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func loadFixtures(t *testing.T) []job.Job {
	t.Helper()
	open := func(name string) *os.File {
		f, err := os.Open("../fetch/testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return f
	}
	ro, err := fetch.ParseRemoteOK(open("remoteok.json"))
	if err != nil {
		t.Fatal(err)
	}
	gh, err := fetch.ParseGreenhouse(open("greenhouse.json"), "examplecloud")
	if err != nil {
		t.Fatal(err)
	}
	return append(ro, gh...)
}

func TestCheckFixtures(t *testing.T) {
	f := exampleFilter(t)
	want := map[string]string{
		"Senior Rust Engineer":         "",
		"Backend Engineer (Go)":        "", // no location, allowed as unspecified
		"Junior Go Developer":          "title_excluded:junior",
		"Senior PHP Developer":         "", // passes filters, scoring sinks it
		"Senior Golang Engineer":       "keyword_excluded:US only",
		"Rust Developer":               "too_old",
		"Product Manager":              "title_not_included",
		"Senior Software Engineer, Go": "",
		"Systems Engineer, Rust":       "region_denied:hybrid",
		"Senior Backend Engineer":      "", // "Remote; Madrid, Spain"
		"Account Executive":            "title_not_included",
	}
	jobs := loadFixtures(t)
	if len(jobs) != len(want) {
		t.Fatalf("got %d fixture jobs, want %d", len(jobs), len(want))
	}
	for _, j := range jobs {
		w, ok := want[j.Title]
		if !ok {
			t.Errorf("unexpected fixture job %q", j.Title)
			continue
		}
		if got := f.Check(j, now); got != w {
			t.Errorf("Check(%q) = %q, want %q", j.Title, got, w)
		}
	}
}

func TestCheckRules(t *testing.T) {
	f := exampleFilter(t)
	base := job.Job{
		Title:       "Senior Backend Engineer",
		Location:    "Remote - Europe",
		Description: "We write Rust.",
		PostedAt:    now.Add(-24 * time.Hour),
	}
	tests := []struct {
		name   string
		modify func(*job.Job)
		want   string
	}{
		{"passes", func(*job.Job) {}, ""},
		{"unknown posting date is kept", func(j *job.Job) { j.PostedAt = time.Time{} }, ""},
		{"just inside max age", func(j *job.Job) { j.PostedAt = now.Add(-7*24*time.Hour + time.Minute) }, ""},
		{"too old", func(j *job.Job) { j.PostedAt = now.Add(-8 * 24 * time.Hour) }, "too_old"},
		{"US location", func(j *job.Job) { j.Location = "San Francisco, CA" }, "region_not_allowed"},
		{"worldwide", func(j *job.Job) { j.Location = "Worldwide" }, ""},
		{"on-site", func(j *job.Job) { j.Location = "Berlin, Germany (onsite)" }, "region_denied:on-site"},
		{"intern", func(j *job.Job) { j.Title = "Software Engineer Intern" }, "title_excluded:intern"},
		{"no Go or Rust", func(j *job.Job) { j.Description = "Java and Spring." }, "missing_required_keyword"},
		{"lowercase go is not the language", func(j *job.Job) { j.Description = "Ready to go!" }, "missing_required_keyword"},
		{"tag satisfies required", func(j *job.Job) { j.Description = ""; j.Tags = []string{"golang"} }, ""},
		{"clearance", func(j *job.Job) { j.Description += " Requires security clearance." }, "keyword_excluded:security clearance"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := base
			tt.modify(&j)
			if got := f.Check(j, now); got != tt.want {
				t.Errorf("Check = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnspecifiedLocationCanBeRejected(t *testing.T) {
	var cfg config.Filter
	cfg.Regions.Allow = []string{"europe"}
	f, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Check(job.Job{Title: "x"}, now); got != "region_unspecified" {
		t.Errorf("Check = %q, want region_unspecified", got)
	}
}

func TestMaxAgeBySourceKind(t *testing.T) {
	f := exampleFilter(t)
	old := job.Job{
		Title:       "Senior Backend Engineer",
		Location:    "Remote - Europe",
		Description: "Rust",
		PostedAt:    now.Add(-30 * 24 * time.Hour),
	}
	for source, want := range map[string]string{
		"remoteok":          "too_old", // default 7d
		"greenhouse:gitlab": "",        // 60d override, looked up by kind
		"ashby:supabase":    "",
		"hn":                "", // 35d
		"lever":             "",
		"unknown:board":     "too_old",
	} {
		j := old
		j.Source = source
		if got := f.Check(j, now); got != want {
			t.Errorf("%s: Check = %q, want %q", source, got, want)
		}
	}
}
