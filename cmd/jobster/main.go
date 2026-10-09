// Command jobster fetches remote job postings, filters and scores them, and
// reports new matches. It runs once per invocation, for use from cron or CI.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"strings"
	"time"

	"github.com/0xataru/jobster/internal/config"
	"github.com/0xataru/jobster/internal/fetch"
	"github.com/0xataru/jobster/internal/filter"
	"github.com/0xataru/jobster/internal/job"
	"github.com/0xataru/jobster/internal/notify"
	"github.com/0xataru/jobster/internal/score"
	"github.com/0xataru/jobster/internal/store"
)

func main() {
	configPath := flag.String("config", "jobster.yaml", "path to the YAML config")
	dryRun := flag.Bool("dry-run", false, "print matches without touching the database")
	verbose := flag.Bool("v", false, "log every dropped job and its reason")
	notifyMode := flag.String("notify", "auto", "where to send the digest: auto (telegram if configured, else stdout), telegram, stdout")
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, *configPath, *dryRun, *notifyMode); err != nil {
		slog.Error("run failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configPath string, dryRun bool, notifyMode string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	notifier, err := buildNotifier(cfg.Telegram, notifyMode, dryRun)
	if err != nil {
		return err
	}
	flt, err := filter.New(cfg.Filter)
	if err != nil {
		return fmt.Errorf("filter: %w", err)
	}
	scorer, err := score.New(cfg.Scoring, cfg.Companies)
	if err != nil {
		return fmt.Errorf("scoring: %w", err)
	}

	start := time.Now()
	results := fetch.Run(ctx, buildFetchers(cfg), cfg.Workers, cfg.HTTPTimeout.D())
	var fetched []job.Job
	failed := 0
	for _, r := range results {
		if r.Err != nil {
			failed++
			slog.Warn("source failed", "source", r.Source, "took", r.Took.Round(time.Millisecond), "err", r.Err)
			continue
		}
		slog.Info("source ok", "source", r.Source, "jobs", len(r.Jobs), "took", r.Took.Round(time.Millisecond))
		fetched = append(fetched, r.Jobs...)
	}
	if len(results) > 0 && failed == len(results) {
		return errors.New("every source failed")
	}

	matches := evaluate(fetched, flt, scorer, start)

	if dryRun {
		var top []job.Scored
		for _, m := range matches {
			if m.Score >= cfg.Scoring.Threshold {
				top = append(top, m)
			}
		}
		return notifier.Notify(ctx, top[:min(len(top), cfg.DigestLimit)])
	}

	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	if n, err := st.Prune(ctx, start.Add(-cfg.DedupeWindow.D())); err != nil {
		return fmt.Errorf("prune: %w", err)
	} else if n > 0 {
		slog.Info("pruned stale jobs", "count", n)
	}
	added, err := st.Upsert(ctx, matches, start)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	pending, err := st.Pending(ctx, start, cfg.Scoring.Threshold, cfg.DigestLimit)
	if err != nil {
		return fmt.Errorf("pending: %w", err)
	}
	slog.Info("stored", "new", added, "to_notify", len(pending))

	if err := notifier.Notify(ctx, pending); err != nil {
		return fmt.Errorf("notify: %w", err) // jobs stay pending and are retried next run
	}
	return st.MarkNotified(ctx, pending, time.Now())
}

// buildNotifier picks the digest destination. A dry run always prints, so
// tuning the config never messages the chat.
func buildNotifier(tg config.Telegram, mode string, dryRun bool) (notify.Notifier, error) {
	stdout := notify.Writer{W: os.Stdout}
	if dryRun || mode == "stdout" {
		return stdout, nil
	}
	if mode != "auto" && mode != "telegram" {
		return nil, fmt.Errorf("-notify: unknown mode %q (want auto, telegram or stdout)", mode)
	}

	token, chatID := os.Getenv(tg.TokenEnv), os.Getenv(tg.ChatIDEnv)
	switch {
	case token != "" && chatID != "":
		slog.Info("notifying via telegram")
		return &notify.Telegram{Token: token, ChatID: chatID}, nil
	case mode == "telegram" || token != "" || chatID != "":
		// Falling back to stdout here would mark jobs as notified that
		// nobody received, so a partial or required setup is an error.
		return nil, fmt.Errorf("telegram needs both $%s and $%s set", tg.TokenEnv, tg.ChatIDEnv)
	}
	return stdout, nil
}

func buildFetchers(cfg *config.Config) []fetch.Fetcher {
	client := fetch.NewClient()
	var fs []fetch.Fetcher
	if cfg.Sources.RemoteOK.Enabled {
		fs = append(fs, &fetch.RemoteOK{Client: client})
	}
	if cfg.Sources.HackerNews.Enabled {
		fs = append(fs, &fetch.HackerNews{Client: client})
	}
	if cfg.Sources.Remotive.Enabled {
		fs = append(fs, &fetch.Remotive{Client: client})
	}
	if s := cfg.Sources.WeWorkRemotely; s.Enabled {
		for _, c := range s.Categories {
			fs = append(fs, &fetch.WeWorkRemotely{Client: client, Category: c})
		}
	}
	if s := cfg.Sources.Himalayas; s.Enabled {
		for _, q := range s.Queries {
			fs = append(fs, &fetch.Himalayas{Client: client, Query: q, Pages: s.Pages})
		}
	}
	if s := cfg.Sources.Djinni; s.Enabled {
		for _, k := range s.Keywords {
			fs = append(fs, &fetch.Djinni{Client: client, Keyword: k, Params: s.Params})
		}
	}
	for _, c := range cfg.Companies {
		switch c.ATS {
		case "greenhouse":
			fs = append(fs, &fetch.Greenhouse{Client: client, Board: c.Slug})
		case "lever":
			fs = append(fs, &fetch.Lever{Client: client, Board: c.Slug, Company: c.Name})
		case "ashby":
			fs = append(fs, &fetch.Ashby{Client: client, Board: c.Slug, Company: c.Name})
		case "teamtailor":
			fs = append(fs, &fetch.Teamtailor{Client: client, Board: c.Slug, Company: c.Name})
		}
	}
	return fs
}

// evaluate filters and scores jobs, collapses duplicates (keeping the best
// score), and returns them best first.
func evaluate(jobs []job.Job, flt *filter.Filter, scorer *score.Scorer, now time.Time) []job.Scored {
	dropped := map[string]int{}
	best := map[string]job.Scored{}
	for _, j := range jobs {
		if reason := flt.Check(j, now); reason != "" {
			slog.Debug("dropped", "reason", reason, "title", j.Title, "company", j.Company, "location", j.Location)
			rule, _, _ := strings.Cut(reason, ":")
			dropped[rule]++
			continue
		}
		s := scorer.Score(j)
		if prev, ok := best[j.Key()]; !ok || s.Score > prev.Score {
			best[j.Key()] = s
		}
	}
	slog.Info("filtered", "fetched", len(jobs), "kept", len(best), "dropped", dropped)

	out := make([]job.Scored, 0, len(best))
	for _, s := range best {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b job.Scored) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), b.PostedAt.Compare(a.PostedAt), strings.Compare(a.URL, b.URL))
	})
	return out
}
