package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/0xataru/jobster/internal/job"
	"github.com/0xataru/workerpool"
)

const (
	careersMaxJobs  = 40 // job pages read per company and run
	careersParallel = 4
)

// Careers finds a company's open roles from its careers page, whatever
// hosts them. If the page links to a known job board (Greenhouse, Ashby,
// Lever, Teamtailor, Workable, Recruitee, Personio), that board's API is
// used. Otherwise, or if that API fails, it reads the page itself: schema.org
// JobPosting data when present, else each linked job page's title and text.
type Careers struct {
	Client  *Client
	Company string
	URL     string
	// LinkPattern selects job links on the careers page by their URL
	// path. When nil, links below the careers path or under /jobs/,
	// /positions/ and similar are used.
	LinkPattern *regexp.Regexp
}

func (c *Careers) Name() string { return "careers:" + slugify(c.Company) }

// Discovery describes how a careers page will be read.
type Discovery struct {
	ATS      ATS    // the detected board, when HasATS
	HasATS   bool   //
	Postings int    // JobPosting entries embedded in the page
	Links    int    // job links found on the page
	Followed string // openings page followed from the careers page, if any
}

// Discover inspects the careers page without fetching any jobs.
func (c *Careers) Discover(ctx context.Context) (Discovery, error) {
	doc, final, err := c.load(ctx)
	if err != nil {
		return Discovery{}, err
	}
	var d Discovery
	if final.String() != c.URL {
		d.Followed = final.String()
	}
	d.ATS, d.HasATS = DetectATS(doc, final)
	p := parsePage(doc, final)
	d.Postings = len(jobPostings(p.JSONLD))
	d.Links = len(c.jobLinks(p, final))
	return d, nil
}

func (c *Careers) Fetch(ctx context.Context) ([]job.Job, error) {
	doc, final, err := c.load(ctx)
	if err != nil {
		return nil, err
	}

	var boardErr error
	if a, ok := DetectATS(doc, final); ok {
		f, err := NewATSFetcher(c.Client, a, c.Company)
		if err == nil {
			jobs, err := f.Fetch(ctx)
			if err == nil {
				return jobs, nil
			}
		}
		boardErr = fmt.Errorf("detected %s board failed (%w); reading the page instead", a, err)
	}

	jobs, err := c.fromPage(ctx, doc, final)
	if err != nil {
		return nil, errors.Join(boardErr, err)
	}
	return jobs, nil
}

// openingsLink matches link text that leads from a careers landing page to
// the actual list of jobs.
var openingsLink = regexp.MustCompile(`(?i)\b(?:open (?:positions|roles|jobs)|job openings|current (?:openings|vacancies)|` +
	`(?:see|view|browse|explore|show) (?:all |our |open |current )*(?:jobs|openings|positions|roles|vacancies)|all (?:jobs|openings|positions))\b`)

// load fetches the careers page. When the page names no job board and has
// no JobPosting data but links to an openings page ("Browse open
// positions"), that page is followed once and returned instead.
func (c *Careers) load(ctx context.Context) (string, *url.URL, error) {
	doc, final, err := c.Client.getPage(ctx, c.URL)
	if err != nil {
		return "", nil, err
	}
	if _, ok := DetectATS(doc, final); ok {
		return doc, final, nil
	}
	p := parsePage(doc, final)
	if len(jobPostings(p.JSONLD)) > 0 {
		return doc, final, nil
	}
	for _, l := range p.Links {
		if !openingsLink.MatchString(l.Text) || l.URL.String() == final.String() {
			continue
		}
		next, nextFinal, err := c.Client.getPage(ctx, l.URL.String())
		if err != nil {
			break // keep the landing page; its links may still work
		}
		return next, nextFinal, nil
	}
	return doc, final, nil
}

func (c *Careers) fromPage(ctx context.Context, doc string, final *url.URL) ([]job.Job, error) {
	p := parsePage(doc, final)
	if postings := jobPostings(p.JSONLD); len(postings) > 0 {
		jobs := make([]job.Job, 0, len(postings))
		for _, jp := range postings {
			jobs = append(jobs, jobFromPosting(jp, c.Name(), c.Company, final.String()))
		}
		return jobs, nil
	}

	links := c.jobLinks(p, final)
	if len(links) == 0 {
		return nil, fmt.Errorf("no job links on %s (rendered by JavaScript? set link_pattern, or ats and slug)", c.URL)
	}
	results := workerpool.Map(ctx, careersParallel, links, func(ctx context.Context, l pageLink) (pageJob, error) {
		return c.jobFromPage(ctx, l)
	})
	// A title shared by several job pages is the site's page name ("Job
	// Openings"), not a job title.
	titles := map[string]int{}
	for _, r := range results {
		if r.Err == nil && r.Value.fromMeta {
			titles[strings.ToLower(r.Value.Title)]++
		}
	}
	var jobs []job.Job
	var errs []error
	for _, r := range results {
		if r.Err != nil {
			errs = append(errs, r.Err)
			continue
		}
		j := r.Value.Job
		if r.Value.fromMeta && (titles[strings.ToLower(j.Title)] > 1 || genericTitle(j.Title, c.Company)) && r.Input.Text != "" {
			j.Title = r.Input.Text
		}
		jobs = append(jobs, j)
	}
	// Keep partial results: one dead link shouldn't hide the other roles.
	if len(jobs) == 0 {
		return nil, fmt.Errorf("all %d job pages failed: %w", len(links), errors.Join(errs...))
	}
	return jobs, nil
}

// pageJob is a job read from its own page; fromMeta marks a title taken from
// page metadata rather than JobPosting data, which may be the site's generic
// page name.
type pageJob struct {
	job.Job
	fromMeta bool
}

var genericTitles = regexp.MustCompile(`(?i)^(?:job openings?|open (?:positions|roles|jobs)|find open positions|current openings|careers?|jobs|join (?:us|our team)|work with us|vacancies)$`)

func genericTitle(title, company string) bool {
	t := strings.TrimSpace(title)
	return t == "" || genericTitles.MatchString(t) || strings.EqualFold(t, company)
}

func (c *Careers) jobFromPage(ctx context.Context, l pageLink) (pageJob, error) {
	doc, final, err := c.Client.getPage(ctx, l.URL.String())
	if err != nil {
		return pageJob{}, err
	}
	p := parsePage(doc, final)
	if postings := jobPostings(p.JSONLD); len(postings) > 0 {
		return pageJob{Job: jobFromPosting(postings[0], c.Name(), c.Company, final.String())}, nil
	}
	title := p.OGTitle
	if title == "" {
		title = p.Title
	}
	title = stripCompanySuffix(title, c.Company)
	return pageJob{Job: job.Job{
		Source:      c.Name(),
		ExternalID:  final.String(),
		Title:       title,
		Company:     c.Company,
		Description: strings.TrimSpace(p.Description + "\n" + p.Text),
		URL:         final.String(),
	}, fromMeta: true}, nil
}

// Path segments that usually hold individual job pages.
var jobPathRe = regexp.MustCompile(`(?i)/(jobs?|careers?|positions?|openings?|vacanc(?:y|ies)|roles?|o)/[^/]+`)

// jobLinks picks the links on a careers page that lead to single jobs on the
// same site.
func (c *Careers) jobLinks(p page, careers *url.URL) []pageLink {
	base := strings.TrimSuffix(careers.Path, "/")
	seen := map[string]bool{careers.String(): true}
	var out []pageLink
	for _, l := range p.Links {
		u := l.URL
		if !sameSite(u.Hostname(), careers.Hostname()) {
			continue
		}
		p := strings.TrimSuffix(u.Path, "/")
		if internalPath(p) {
			continue
		}
		var ok bool
		if c.LinkPattern != nil {
			ok = c.LinkPattern.MatchString(u.Path)
		} else {
			below := base != "" && strings.HasPrefix(p, base+"/") && path.Base(p) != ""
			ok = below || jobPathRe.MatchString(p)
		}
		key := u.String()
		if !ok || p == base || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, l)
		if len(out) == careersMaxJobs {
			break
		}
	}
	return out
}

// internalPath reports framework data files and static assets, which sit
// below careers paths but are never job pages.
func internalPath(p string) bool {
	if strings.HasPrefix(path.Base(p), "_") || strings.Contains(p, "/_") {
		return true
	}
	switch strings.ToLower(path.Ext(p)) {
	case ".js", ".json", ".css", ".xml", ".yml", ".yaml", ".md", ".txt", ".zip",
		".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".ico", ".pdf", ".woff", ".woff2":
		return true
	}
	return false
}

// sameSite reports whether host is site or one of its subdomains, or the
// other way round (careers.acme.com linking to acme.com/jobs/...).
func sameSite(host, site string) bool {
	host, site = strings.ToLower(host), strings.ToLower(site)
	return host == site || strings.HasSuffix(host, "."+site) || strings.HasSuffix(site, "."+host)
}

// stripCompanySuffix turns "Backend Engineer | Acme" into "Backend Engineer".
func stripCompanySuffix(title, company string) string {
	title = strings.TrimSpace(title)
	for _, sep := range []string{" | ", " - ", " — ", " – ", " at "} {
		if i := strings.LastIndex(title, sep); i > 0 &&
			strings.Contains(strings.ToLower(title[i:]), strings.ToLower(company)) {
			return strings.TrimSpace(title[:i])
		}
	}
	return title
}

func slugify(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), "-")
}

// getPage GETs an HTML page and returns it with its final URL after
// redirects, which relative links resolve against.
func (c *Client) getPage(ctx context.Context, rawURL string) (string, *url.URL, error) {
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return "", nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return "", nil, fmt.Errorf("GET %s: %w", rawURL, err)
	}
	final, _ := url.Parse(rawURL)
	if lb, ok := body.(limitedBody); ok && lb.url != nil {
		final = lb.url
	}
	return string(data), final, nil
}
