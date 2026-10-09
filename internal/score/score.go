// Package score ranks jobs that passed the hard filters.
package score

import (
	"fmt"
	"strings"

	"github.com/0xataru/jobster/internal/config"
	"github.com/0xataru/jobster/internal/job"
	"github.com/0xataru/jobster/internal/match"
)

// Scorer assigns each job a score from weighted keyword and salary rules.
type Scorer struct {
	title, text   []match.Weighted
	minSalary     int
	currencies    map[string]bool
	salaryAbove   int
	salaryBelow   int
	targets       map[string]bool
	targetCompany int
}

// New compiles the scoring rules; companies are the target companies.
func New(cfg config.Scoring, companies []config.Company) (*Scorer, error) {
	title, err := match.NewWeighted(cfg.Title)
	if err != nil {
		return nil, err
	}
	text, err := match.NewWeighted(cfg.Text)
	if err != nil {
		return nil, err
	}
	currencies := make(map[string]bool, len(cfg.SalaryCurrencies))
	for _, c := range cfg.SalaryCurrencies {
		currencies[strings.ToUpper(c)] = true
	}
	targets := make(map[string]bool, len(companies))
	for _, c := range companies {
		targets[job.NormalizeCompany(c.Name)] = true
	}
	return &Scorer{
		title:         title,
		text:          text,
		minSalary:     cfg.MinSalary,
		currencies:    currencies,
		salaryAbove:   cfg.SalaryAbove,
		salaryBelow:   cfg.SalaryBelow,
		targets:       targets,
		targetCompany: cfg.TargetCompany,
	}, nil
}

// Score rates j. Each term counts at most once; Reasons lists every rule
// that contributed, e.g. "+5 title:rust" or "-2 php".
func (s *Scorer) Score(j job.Job) job.Scored {
	out := job.Scored{Job: j}
	add := func(w int, format string, args ...any) {
		if w == 0 {
			return
		}
		out.Score += w
		out.Reasons = append(out.Reasons, fmt.Sprintf("%+d ", w)+fmt.Sprintf(format, args...))
	}

	for _, t := range s.title {
		if t.In(j.Title) {
			add(t.Weight, "title:%s", t.Name)
		}
	}
	tags := strings.Join(j.Tags, " ")
	for _, t := range s.text {
		if t.In(j.Description, tags) {
			add(t.Weight, "%s", t.Name)
		}
	}

	// An unknown currency is assumed comparable; a known one must be listed.
	comparable := j.Currency == "" || s.currencies[strings.ToUpper(j.Currency)]
	if s.minSalary > 0 && comparable {
		if top := max(j.SalaryMin, j.SalaryMax); top > 0 {
			if top >= s.minSalary {
				add(s.salaryAbove, "salary %d", top)
			} else {
				add(s.salaryBelow, "salary %d", top)
			}
		}
	}

	if s.targets[job.NormalizeCompany(j.Company)] {
		add(s.targetCompany, "target company")
	}
	return out
}
