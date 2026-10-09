// Package filter applies the hard filters that drop unsuitable jobs.
package filter

import (
	"strings"
	"time"

	"github.com/0xataru/jobster/internal/config"
	"github.com/0xataru/jobster/internal/job"
	"github.com/0xataru/jobster/internal/match"
)

// Filter decides whether a job is worth scoring at all.
type Filter struct {
	titleInclude, titleExclude match.Set
	required, textExclude      match.Set
	regionAllow, regionDeny    match.Set
	allowUnspecified           bool
	maxAge                     time.Duration
	maxAgeBySource             map[string]time.Duration
}

// New compiles the filter rules from cfg.
func New(cfg config.Filter) (*Filter, error) {
	f := &Filter{
		allowUnspecified: cfg.Regions.AllowUnspecified,
		maxAge:           cfg.MaxAge.D(),
		maxAgeBySource:   make(map[string]time.Duration, len(cfg.MaxAgeBySource)),
	}
	for kind, d := range cfg.MaxAgeBySource {
		f.maxAgeBySource[kind] = d.D()
	}
	for _, s := range []struct {
		dst  *match.Set
		raws []string
	}{
		{&f.titleInclude, cfg.Titles.Include},
		{&f.titleExclude, cfg.Titles.Exclude},
		{&f.required, cfg.Keywords.Required},
		{&f.textExclude, cfg.Keywords.Exclude},
		{&f.regionAllow, cfg.Regions.Allow},
		{&f.regionDeny, cfg.Regions.Deny},
	} {
		set, err := match.NewSet(s.raws)
		if err != nil {
			return nil, err
		}
		*s.dst = set
	}
	return f, nil
}

// Check returns "" if j passes every filter, or else the reason it was
// dropped, formatted "rule" or "rule:term".
func (f *Filter) Check(j job.Job, now time.Time) string {
	if maxAge := f.maxAgeFor(j.Source); maxAge > 0 && !j.PostedAt.IsZero() && now.Sub(j.PostedAt) > maxAge {
		return "too_old"
	}
	if len(f.titleInclude) > 0 {
		if _, ok := f.titleInclude.First(j.Title); !ok {
			return "title_not_included"
		}
	}
	if t, ok := f.titleExclude.First(j.Title); ok {
		return "title_excluded:" + t
	}
	if t, ok := f.regionDeny.First(j.Location); ok {
		return "region_denied:" + t
	}
	if strings.TrimSpace(j.Location) == "" {
		if !f.allowUnspecified {
			return "region_unspecified"
		}
	} else if _, ok := f.regionAllow.First(j.Location); !ok && len(f.regionAllow) > 0 {
		return "region_not_allowed"
	}
	if t, ok := f.textExclude.First(j.Title, j.Location, j.Description); ok {
		return "keyword_excluded:" + t
	}
	if len(f.required) > 0 {
		if _, ok := f.required.First(j.Title, j.Description, strings.Join(j.Tags, " ")); !ok {
			return "missing_required_keyword"
		}
	}
	return ""
}

// maxAgeFor returns the age limit for a source such as "greenhouse:gitlab",
// looked up by its kind ("greenhouse").
func (f *Filter) maxAgeFor(source string) time.Duration {
	kind, _, _ := strings.Cut(source, ":")
	if d, ok := f.maxAgeBySource[kind]; ok {
		return d
	}
	return f.maxAge
}
