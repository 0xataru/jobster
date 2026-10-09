package fetch

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseRemoteOK(t *testing.T) {
	f, err := os.Open("testdata/remoteok.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	jobs, err := ParseRemoteOK(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 7 {
		t.Fatalf("got %d jobs, want 7 (legal notice must be skipped)", len(jobs))
	}

	j := jobs[0]
	if j.Source != "remoteok" || j.ExternalID != "1001" || j.Title != "Senior Rust Engineer" ||
		j.Company != "Ferrous Systems" || j.Location != "Europe" {
		t.Errorf("unexpected fields: %+v", j)
	}
	if j.SalaryMin != 90000 || j.SalaryMax != 130000 || j.Currency != "USD" {
		t.Errorf("salary = %d-%d %q", j.SalaryMin, j.SalaryMax, j.Currency)
	}
	if want := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC); !j.PostedAt.Equal(want) {
		t.Errorf("PostedAt = %v, want %v", j.PostedAt, want)
	}
	if j.URL != "https://remoteOK.com/remote-jobs/senior-rust-engineer-ferrous-1001" {
		t.Errorf("URL = %q", j.URL)
	}
	if strings.Contains(j.Description, "<") || !strings.Contains(j.Description, "distributed storage") {
		t.Errorf("Description not reduced to text: %q", j.Description)
	}

	mojibake := jobs[1]
	if mojibake.Company != "Müller Systems" {
		t.Errorf("Company = %q, want mojibake repaired", mojibake.Company)
	}
	if !strings.Contains(mojibake.Description, "für uns") {
		t.Errorf("Description = %q, want mojibake repaired", mojibake.Description)
	}
	if mojibake.Currency != "" {
		t.Errorf("Currency = %q for unknown salary", mojibake.Currency)
	}
}

func TestParseGreenhouse(t *testing.T) {
	f, err := os.Open("testdata/greenhouse.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	jobs, err := ParseGreenhouse(f, "examplecloud")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 4 {
		t.Fatalf("got %d jobs, want 4", len(jobs))
	}

	j := jobs[0]
	if j.Source != "greenhouse:examplecloud" || j.ExternalID != "101" || j.Company != "Example Cloud, Inc." {
		t.Errorf("unexpected fields: %+v", j)
	}
	if j.Description != "About the role\nBuild distributed systems in Go & Kubernetes." {
		t.Errorf("Description = %q", j.Description)
	}
	if want := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC); !j.PostedAt.Equal(want) {
		t.Errorf("PostedAt = %v, want first_published %v", j.PostedAt, want)
	}

	if got := jobs[1].Location; got != "Hybrid; London, United Kingdom" {
		t.Errorf("Location = %q, want office appended", got)
	}
	if want := time.Date(2026, 10, 8, 14, 0, 0, 0, time.UTC); !jobs[2].PostedAt.Equal(want) {
		t.Errorf("PostedAt = %v, want updated_at fallback %v", jobs[2].PostedAt, want)
	}
}

func TestFixMojibake(t *testing.T) {
	for in, want := range map[string]string{
		"fÃ¼r":      "für",
		"für":       "für",       // already correct
		"São Paulo": "São Paulo", // Latin-1 range but not mojibake
		"plain":     "plain",
		"MÃ¼ller ✓": "MÃ¼ller ✓", // mixed with non-Latin-1, left alone
	} {
		if got := fixMojibake(in); got != want {
			t.Errorf("fixMojibake(%q) = %q, want %q", in, got, want)
		}
	}
}
