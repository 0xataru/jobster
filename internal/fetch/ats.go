package fetch

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// ATSKinds lists the applicant tracking systems with a public job feed.
var ATSKinds = []string{"greenhouse", "ashby", "lever", "teamtailor", "workable", "recruitee", "personio"}

// ATS identifies a company's board on an applicant tracking system.
type ATS struct {
	Kind string // one of ATSKinds
	Slug string
	EU   bool   // board hosted in the EU region (lever, greenhouse)
	TLD  string // personio: "de" or "com"
	// BaseURL overrides the API root, for boards served from the company's
	// own domain (Recruitee career sites on custom domains).
	BaseURL string
}

func (a ATS) String() string { return a.Kind + ":" + a.Slug }

// Pinnable reports whether the board can be configured as plain ats and
// slug. Boards on a company's own domain, in the EU region, or on
// personio.com need the careers URL to be found.
func (a ATS) Pinnable() bool { return a.BaseURL == "" && !a.EU && a.TLD != "com" }

// NewATSFetcher returns the fetcher for a company's board.
func NewATSFetcher(c *Client, a ATS, company string) (Fetcher, error) {
	switch a.Kind {
	case "greenhouse":
		return &Greenhouse{Client: c, Board: a.Slug}, nil
	case "ashby":
		return &Ashby{Client: c, Board: a.Slug, Company: company}, nil
	case "lever":
		l := &Lever{Client: c, Board: a.Slug, Company: company}
		if a.EU {
			l.BaseURL = "https://api.eu.lever.co"
		}
		return l, nil
	case "teamtailor":
		return &Teamtailor{Client: c, Board: a.Slug, Company: company}, nil
	case "workable":
		return &Workable{Client: c, Board: a.Slug, Company: company}, nil
	case "recruitee":
		return &Recruitee{Client: c, Board: a.Slug, Company: company, BaseURL: a.BaseURL}, nil
	case "personio":
		p := &Personio{Client: c, Board: a.Slug, Company: company}
		if a.TLD == "com" {
			p.BaseURL = "https://" + a.Slug + ".jobs.personio.com"
		}
		return p, nil
	}
	return nil, fmt.Errorf("unknown ats %q", a.Kind)
}

var atsPatterns = []struct {
	kind string
	re   *regexp.Regexp // group 1: EU marker or TLD, group 2: slug
}{
	{"greenhouse", regexp.MustCompile(`(?i)(?:job-)?boards(\.eu)?\.greenhouse\.io/(?:embed/job_board(?:/js)?\?for=)?([a-z0-9_-]+)`)},
	{"ashby", regexp.MustCompile(`(?i)jobs\.ashbyhq\.com/()([a-z0-9_.%-]+)`)},
	{"lever", regexp.MustCompile(`(?i)jobs(\.eu)?\.lever\.co/([a-z0-9_-]+)`)},
	{"teamtailor", regexp.MustCompile(`(?i)https?://()([a-z0-9-]+)\.teamtailor\.com`)},
	{"workable", regexp.MustCompile(`(?i)apply\.workable\.com/()([a-z0-9_-]+)`)},
	{"recruitee", regexp.MustCompile(`(?i)https?://()([a-z0-9-]+)\.recruitee\.com`)},
	{"personio", regexp.MustCompile(`(?i)https?://([a-z0-9-]+)\.jobs\.personio\.(de|com)`)},
}

// Path segments and subdomains that look like slugs but aren't boards.
var notSlugs = map[string]bool{
	"api": true, "app": true, "www": true, "cdn": true, "assets": true, "static": true,
	"embed": true, "j": true, "jobs": true, "careers": true, "career": true, "widget": true,
	"js": true, "css": true, "images": true, "img": true, "favicon.ico": true,
}

// recruiteeHosted marks a career site served by Recruitee on the company's
// own domain; its offers API lives on that domain too.
var recruiteeHosted = regexp.MustCompile(`(?i)\brecruiteecdn\.com\b`)

// DetectATS finds the job board behind a careers page at pageURL: the page
// itself when Recruitee serves it, else the most frequently linked board.
// It returns ok=false when there is none.
func DetectATS(page string, pageURL *url.URL) (ATS, bool) {
	if pageURL != nil && recruiteeHosted.MatchString(page) {
		root := pageURL.Scheme + "://" + pageURL.Host
		return ATS{Kind: "recruitee", Slug: pageURL.Hostname(), BaseURL: root}, true
	}
	counts := map[ATS]int{}
	for _, p := range atsPatterns {
		for _, m := range p.re.FindAllStringSubmatch(page, -1) {
			a := ATS{Kind: p.kind, Slug: m[2]}
			switch p.kind {
			case "personio": // slug and TLD are captured the other way round
				a.Slug, a.TLD = m[1], m[2]
			case "greenhouse", "lever":
				a.EU = m[1] != ""
			}
			if notSlugs[strings.ToLower(a.Slug)] || strings.Contains(a.Slug, "analytics") || strings.Contains(a.Slug, "cdn") {
				continue
			}
			counts[a]++
		}
	}
	if len(counts) == 0 {
		return ATS{}, false
	}
	found := make([]ATS, 0, len(counts))
	for a := range counts {
		found = append(found, a)
	}
	sort.Slice(found, func(i, j int) bool {
		if counts[found[i]] != counts[found[j]] {
			return counts[found[i]] > counts[found[j]]
		}
		return found[i].String() < found[j].String()
	})
	return found[0], true
}
