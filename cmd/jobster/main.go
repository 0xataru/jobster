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
	"regexp"
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
	discoverOnly := flag.Bool("discover", false, "show how each company's careers page will be read, then exit")
	notifyMode := flag.String("notify", "auto", "where to send the digest: auto (telegram if configured, else stdout), telegram, stdout")
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if *discoverOnly {
		cfg, err := config.Load(*configPath)
		if err == nil {
			err = discover(ctx, cfg)
		}
		if err != nil {
			slog.Error("discover failed", "err", err)
			os.Exit(1)
		}
		return
	}
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
		switch {
		case c.ATS != "":
			f, err := fetch.NewATSFetcher(client, fetch.ATS{Kind: c.ATS, Slug: c.Slug}, c.Name)
			if err != nil { // config validation already rejects unknown kinds
				slog.Warn("skipping company", "company", c.Name, "err", err)
				continue
			}
			fs = append(fs, f)
		case c.Careers != "":
			fs = append(fs, careersFetcher(client, c))
		}
	}
	return fs
}

func careersFetcher(client *fetch.Client, c config.Company) *fetch.Careers {
	f := &fetch.Careers{Client: client, Company: c.Name, URL: c.Careers}
	if c.LinkPattern != "" {
		f.LinkPattern = regexp.MustCompile(c.LinkPattern) // validated at load
	}
	return f
}

// discover reports, for each company with a careers page, how jobster will
// read it, so a detected board can be pinned with ats and slug.
func discover(ctx context.Context, cfg *config.Config) error {
	client := fetch.NewClient()
	for _, c := range cfg.Companies {
		switch {
		case c.ATS != "":
			fmt.Printf("%-24s %s:%s (pinned)\n", c.Name, c.ATS, c.Slug)
		case c.Careers == "":
			fmt.Printf("%-24s bonus only (no ats or careers)\n", c.Name)
		default:
			ctx, cancel := context.WithTimeout(ctx, cfg.HTTPTimeout.D())
			d, err := careersFetcher(client, c).Discover(ctx)
			cancel()
			if err == nil && d.Followed != "" {
				fmt.Printf("%-24s followed openings link to %s\n", c.Name, d.Followed)
			}
			switch {
			case err != nil:
				fmt.Printf("%-24s error: %v\n", c.Name, err)
			case d.HasATS && d.ATS.Pinnable():
				fmt.Printf("%-24s %s board detected; to skip detection: { ats: %s, slug: %s }\n", c.Name, d.ATS, d.ATS.Kind, d.ATS.Slug)
			case d.HasATS:
				fmt.Printf("%-24s %s board detected; keep the careers URL to reach it\n", c.Name, d.ATS)
			case d.Postings > 0:
				fmt.Printf("%-24s page with %d embedded JobPosting entries\n", c.Name, d.Postings)
			case d.Links > 0:
				fmt.Printf("%-24s page with %d job links\n", c.Name, d.Links)
			default:
				fmt.Printf("%-24s no board or job links found (JavaScript page? try link_pattern)\n", c.Name)
			}
		}
	}
	return nil
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
