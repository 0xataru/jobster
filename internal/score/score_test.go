package score

import (
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/0xataru/jobster/internal/config"
	"github.com/0xataru/jobster/internal/fetch"
	"github.com/0xataru/jobster/internal/filter"
	"github.com/0xataru/jobster/internal/job"
)

func exampleConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load("../../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestScoreFixtures runs the saved API responses through the example config's
// filters and scoring, pinning the resulting ranking.
func TestScoreFixtures(t *testing.T) {
	cfg := exampleConfig(t)
	flt, err := filter.New(cfg.Filter)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg.Scoring, cfg.Companies)
	if err != nil {
		t.Fatal(err)
	}

	var jobs []job.Job
	for name, parse := range map[string]func(*os.File) ([]job.Job, error){
		"remoteok.json":   func(f *os.File) ([]job.Job, error) { return fetch.ParseRemoteOK(f) },
		"greenhouse.json": func(f *os.File) ([]job.Job, error) { return fetch.ParseGreenhouse(f, "examplecloud") },
	} {
		f, err := os.Open("../fetch/testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parse(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, parsed...)
	}

	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	got := map[string]int{}
	for _, j := range jobs {
		if flt.Check(j, now) == "" {
			got[j.Title] = s.Score(j).Score
		}
	}
	want := map[string]int{
		"Senior Rust Engineer":         14, // rust+senior in title, distributed/k8s/aws, rust tag, salary
		"Backend Engineer (Go)":        10,
		"Senior Software Engineer, Go": 10,
		"Senior Backend Engineer":      7, // no language in title, golang/grpc/postgres in text
		"Senior PHP Developer":         -10,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scores = %v, want %v", got, want)
	}
}

func TestScoreReasons(t *testing.T) {
	cfg := exampleConfig(t)
	s, err := New(cfg.Scoring, []config.Company{{Name: "Example Cloud"}})
	if err != nil {
		t.Fatal(err)
	}
	got := s.Score(job.Job{
		Title:       "Staff Rust Engineer",
		Company:     "Example Cloud, Inc.",
		Description: "Kubernetes, and some PHP.",
		SalaryMax:   70000,
	})
	want := []string{"+5 title:rust", "+2 title:staff", "+1 kubernetes", "-2 php", "-3 salary 70000", "+3 target company"}
	if !slices.Equal(got.Reasons, want) {
		t.Errorf("Reasons = %q, want %q", got.Reasons, want)
	}
	if got.Score != 6 {
		t.Errorf("Score = %d, want 6", got.Score)
	}
}

func TestSalaryInOtherCurrencyIsNeutral(t *testing.T) {
	s, err := New(config.Scoring{MinSalary: 80000, SalaryAbove: 2, SalaryBelow: -3, SalaryCurrencies: []string{"EUR", "usd"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for cur, want := range map[string]int{"INR": 0, "EUR": 2, "USD": 2, "": 2} {
		if got := s.Score(job.Job{SalaryMax: 1600000, Currency: cur}).Score; got != want {
			t.Errorf("currency %q: score %d, want %d", cur, got, want)
		}
	}
}

func TestUnknownSalaryIsNeutral(t *testing.T) {
	s, err := New(config.Scoring{MinSalary: 80000, SalaryAbove: 2, SalaryBelow: -3}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Score(job.Job{Title: "Engineer"}); got.Score != 0 || len(got.Reasons) != 0 {
		t.Errorf("got %d %q, want neutral", got.Score, got.Reasons)
	}
}
