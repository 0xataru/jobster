package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xataru/jobster/internal/job"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func scored(title, company, url string, score int) job.Scored {
	return job.Scored{Job: job.Job{Title: title, Company: company, URL: url, Source: "test"}, Score: score}
}

func TestDedupeAcrossRunsAndSources(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	run1 := time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC)
	run2 := run1.Add(24 * time.Hour)

	added, err := s.Upsert(ctx, []job.Scored{
		scored("Senior Rust Engineer", "Ferrous Systems", "https://remoteok.com/1", 9),
		scored("Backend Engineer", "Acme", "https://acme.example/jobs/1", 7),
	}, run1)
	if err != nil || added != 2 {
		t.Fatalf("run1: added=%d err=%v", added, err)
	}

	added, err = s.Upsert(ctx, []job.Scored{
		// Same role from the company's ATS: matched by company + title.
		scored("Senior Rust Engineer", "Ferrous Systems GmbH", "https://boards.greenhouse.io/ferrous/1", 9),
		// Retitled posting at the same URL: matched by URL.
		scored("Backend Engineer (Go)", "Acme", "https://acme.example/jobs/1", 8),
		scored("Platform Engineer", "Acme", "https://acme.example/jobs/2", 6),
	}, run2)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Errorf("run2: added=%d, want 1", added)
	}
}

func TestPendingUntilNotified(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	run1 := time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC)

	if _, err := s.Upsert(ctx, []job.Scored{
		scored("High", "A", "https://a/1", 9),
		scored("Mid", "A", "https://a/2", 6),
		scored("Low", "A", "https://a/3", 2),
	}, run1); err != nil {
		t.Fatal(err)
	}

	pending, err := s.Pending(ctx, run1, 5, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 || pending[0].Title != "High" || pending[1].Title != "Mid" {
		t.Fatalf("pending = %+v, want High, Mid", pending)
	}
	if limited, _ := s.Pending(ctx, run1, 5, 1); len(limited) != 1 {
		t.Errorf("limit not applied: %d", len(limited))
	}

	// A failed notification leaves jobs pending for the next run.
	run2 := run1.Add(24 * time.Hour)
	if _, err := s.Upsert(ctx, []job.Scored{scored("High", "A", "https://a/1", 9)}, run2); err != nil {
		t.Fatal(err)
	}
	pending, _ = s.Pending(ctx, run2, 5, 10)
	if len(pending) != 1 || pending[0].Title != "High" {
		t.Fatalf("run2 pending = %+v, want High only (Mid is no longer listed)", pending)
	}

	if err := s.MarkNotified(ctx, pending, run2); err != nil {
		t.Fatal(err)
	}
	if pending, _ = s.Pending(ctx, run2, 5, 10); len(pending) != 0 {
		t.Errorf("after MarkNotified pending = %+v", pending)
	}
}

func TestPruneForgetsStaleJobs(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	old := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	s.Upsert(ctx, []job.Scored{scored("Old", "A", "https://a/old", 9)}, old)
	s.Upsert(ctx, []job.Scored{scored("New", "A", "https://a/new", 9)}, recent)

	n, err := s.Prune(ctx, recent.Add(-90*24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("Prune: n=%d err=%v, want 1", n, err)
	}
	// A pruned job counts as new again.
	if added, _ := s.Upsert(ctx, []job.Scored{scored("Old", "A", "https://a/old", 9)}, recent); added != 1 {
		t.Errorf("re-posted job added=%d, want 1", added)
	}
}

func TestReopenKeepsHistory(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "jobs.db")
	now := time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC)

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	s.Upsert(ctx, []job.Scored{scored("X", "A", "https://a/x", 9)}, now)
	s.Close()

	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if added, _ := s.Upsert(ctx, []job.Scored{scored("X", "A", "https://a/x", 9)}, now); added != 0 {
		t.Errorf("added=%d after reopen, want 0", added)
	}
}
