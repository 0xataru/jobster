// Package config loads and validates the YAML configuration.
package config

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration file.
type Config struct {
	DBPath       string   `yaml:"db_path"`
	Workers      int      `yaml:"workers"`
	HTTPTimeout  Duration `yaml:"http_timeout"`
	DedupeWindow Duration `yaml:"dedupe_window"` // forget jobs not seen for this long
	DigestLimit  int      `yaml:"digest_limit"`  // max jobs per notification

	Filter    Filter    `yaml:"filter"`
	Scoring   Scoring   `yaml:"scoring"`
	Companies []Company `yaml:"companies"`
	Sources   Sources   `yaml:"sources"`
	Telegram  Telegram  `yaml:"telegram"`
}

// Filter holds the hard filters; a job failing any of them is dropped.
type Filter struct {
	MaxAge Duration `yaml:"max_age"`
	// MaxAgeBySource overrides MaxAge per source kind ("greenhouse", "hn",
	// ...). ATS boards date jobs by first publication, so long-open roles
	// need a longer window; dedupe still reports each job only once.
	MaxAgeBySource map[string]Duration `yaml:"max_age_by_source"`
	Titles         struct {
		Include []string `yaml:"include"` // title must match one, if set
		Exclude []string `yaml:"exclude"`
	} `yaml:"titles"`
	Keywords struct {
		Required []string `yaml:"required"` // title/description/tags must match one, if set
		Exclude  []string `yaml:"exclude"`  // matched against title, location and description
	} `yaml:"keywords"`
	Regions struct {
		Allow            []string `yaml:"allow"` // location must match one
		Deny             []string `yaml:"deny"`  // location must match none
		AllowUnspecified bool     `yaml:"allow_unspecified"`
	} `yaml:"regions"`
}

// Scoring holds the weights used to rank jobs that passed the filters.
type Scoring struct {
	Threshold int            `yaml:"threshold"` // minimum score to notify
	Title     map[string]int `yaml:"title"`     // terms matched against the title
	Text      map[string]int `yaml:"text"`      // terms matched against description and tags
	MinSalary int            `yaml:"min_salary"`
	// SalaryCurrencies lists the currencies min_salary is compared in;
	// amounts are not converted, so other currencies are neutral.
	SalaryCurrencies []string `yaml:"salary_currencies"`
	SalaryAbove      int      `yaml:"salary_above"` // added when known salary >= min_salary
	SalaryBelow      int      `yaml:"salary_below"` // added when known salary < min_salary
	TargetCompany    int      `yaml:"target_company"`
}

// Company is a favorite company: its jobs get the target_company bonus from
// any source, and its own open roles are fetched when ATS+Slug or Careers is
// set. ATS+Slug reads a known job board directly; Careers starts from the
// careers page, detects the board, and falls back to reading the page.
type Company struct {
	Name string `yaml:"name"`
	ATS  string `yaml:"ats"`  // greenhouse | ashby | lever | teamtailor | workable | recruitee | personio
	Slug string `yaml:"slug"` // board name; for teamtailor, recruitee and personio, the subdomain

	Careers     string `yaml:"careers"`      // careers page URL
	LinkPattern string `yaml:"link_pattern"` // regexp for job links on that page (optional)
}

// Sources toggles the aggregator feeds.
type Sources struct {
	RemoteOK       Toggle    `yaml:"remoteok"`
	HackerNews     Toggle    `yaml:"hackernews"` // latest "Who is hiring?" thread
	Remotive       Toggle    `yaml:"remotive"`
	WeWorkRemotely WWR       `yaml:"weworkremotely"`
	Himalayas      Himalayas `yaml:"himalayas"`
	Djinni         Djinni    `yaml:"djinni"`
}

// WWR selects We Work Remotely category feeds.
type WWR struct {
	Enabled    bool     `yaml:"enabled"`
	Categories []string `yaml:"categories"` // feed names, e.g. remote-back-end-programming-jobs
}

// Himalayas selects Himalayas searches, newest first.
type Himalayas struct {
	Enabled bool     `yaml:"enabled"`
	Queries []string `yaml:"queries"`
	Pages   int      `yaml:"pages"` // pages of 20 results per query
}

// Djinni selects djinni.co feeds, one per primary keyword.
type Djinni struct {
	Enabled  bool              `yaml:"enabled"`
	Keywords []string          `yaml:"keywords"` // primary_keyword values, e.g. Golang
	Params   map[string]string `yaml:"params"`   // extra filters from djinni.co/jobs URLs
}

// Toggle enables or disables a source.
type Toggle struct {
	Enabled bool `yaml:"enabled"`
}

// Telegram names the environment variables holding the bot credentials, so
// secrets never live in the config file.
type Telegram struct {
	TokenEnv  string `yaml:"token_env"`
	ChatIDEnv string `yaml:"chat_id_env"`
}

// Load reads, defaults and validates the config at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Default returns the values used for anything the config file omits.
func Default() *Config {
	cfg := &Config{
		DBPath:       "jobster.db",
		Workers:      4,
		HTTPTimeout:  Duration(30 * time.Second),
		DedupeWindow: Duration(90 * 24 * time.Hour),
		DigestLimit:  20,
		Sources: Sources{
			RemoteOK:   Toggle{Enabled: true},
			HackerNews: Toggle{Enabled: true},
			Remotive:   Toggle{Enabled: true},
			WeWorkRemotely: WWR{Enabled: true, Categories: []string{
				"remote-back-end-programming-jobs",
				"remote-full-stack-programming-jobs",
				"remote-devops-sysadmin-jobs",
			}},
			Himalayas: Himalayas{Enabled: true, Queries: []string{"golang", "rust"}, Pages: 3},
			Djinni: Djinni{Enabled: true, Keywords: []string{"Golang", "Rust"},
				Params: map[string]string{"employment": "remote"}},
		},
		Telegram: Telegram{TokenEnv: "TELEGRAM_BOT_TOKEN", ChatIDEnv: "TELEGRAM_CHAT_ID"},
	}
	cfg.Filter.MaxAge = Duration(7 * 24 * time.Hour)
	cfg.Filter.Regions.AllowUnspecified = true
	cfg.Scoring.Threshold = 5
	cfg.Scoring.SalaryCurrencies = []string{"USD", "EUR", "GBP", "CHF"}
	return cfg
}

var knownATS = map[string]bool{
	"greenhouse": true, "ashby": true, "lever": true, "teamtailor": true,
	"workable": true, "recruitee": true, "personio": true,
}

func (c *Config) validate() error {
	if c.Workers < 1 {
		return fmt.Errorf("workers must be >= 1, got %d", c.Workers)
	}
	if p := c.Sources.Himalayas.Pages; p < 1 || p > 10 {
		return fmt.Errorf("sources.himalayas.pages must be 1-10, got %d", p)
	}
	if c.DigestLimit < 1 {
		return fmt.Errorf("digest_limit must be >= 1, got %d", c.DigestLimit)
	}
	for i, co := range c.Companies {
		if co.Name == "" {
			return fmt.Errorf("companies[%d]: name is required", i)
		}
		if (co.ATS == "") != (co.Slug == "") {
			return fmt.Errorf("company %q: ats and slug must be set together", co.Name)
		}
		if co.ATS != "" && !knownATS[co.ATS] {
			return fmt.Errorf("company %q: unknown ats %q (want greenhouse, ashby, lever, teamtailor, workable, recruitee or personio)", co.Name, co.ATS)
		}
		if co.Careers != "" {
			u, err := url.Parse(co.Careers)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("company %q: careers must be an http(s) URL, got %q", co.Name, co.Careers)
			}
		}
		if co.LinkPattern != "" {
			if co.Careers == "" {
				return fmt.Errorf("company %q: link_pattern needs careers", co.Name)
			}
			if _, err := regexp.Compile(co.LinkPattern); err != nil {
				return fmt.Errorf("company %q: link_pattern: %w", co.Name, err)
			}
		}
	}
	return nil
}

// Duration is a time.Duration that also accepts a day suffix, e.g. "7d".
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	s := strings.TrimSpace(n.Value)
	if days, ok := strings.CutSuffix(s, "d"); ok {
		v, err := strconv.ParseFloat(days, 64)
		if err != nil {
			return fmt.Errorf("line %d: invalid duration %q", n.Line, s)
		}
		*d = Duration(v * float64(24*time.Hour))
		return nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("line %d: invalid duration %q", n.Line, s)
	}
	*d = Duration(v)
	return nil
}

// D returns the value as a time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }
