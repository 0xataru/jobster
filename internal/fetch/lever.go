package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/0xataru/jobster/internal/job"
)

// Lever fetches one company's public Lever postings.
type Lever struct {
	Client  *Client
	Board   string // company slug, e.g. "palantir"
	Company string // display name; Lever's API does not return one
	BaseURL string // defaults to https://api.lever.co
}

func (l *Lever) Name() string { return "lever:" + l.Board }

func (l *Lever) Fetch(ctx context.Context) ([]job.Job, error) {
	base := l.BaseURL
	if base == "" {
		base = "https://api.lever.co"
	}
	u := fmt.Sprintf("%s/v0/postings/%s?mode=json", base, url.PathEscape(l.Board))
	var postings []leverPosting
	if err := l.Client.getJSON(ctx, u, &postings); err != nil {
		return nil, err
	}
	return leverJobs(postings, l.Board, l.Company), nil
}

type leverPosting struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	CreatedAt  int64  `json:"createdAt"` // unix milliseconds
	HostedURL  string `json:"hostedUrl"`
	Country    string `json:"country"` // ISO 3166-1 alpha-2
	Workplace  string `json:"workplaceType"`
	Categories struct {
		Location     string   `json:"location"`
		AllLocations []string `json:"allLocations"`
	} `json:"categories"`
	DescriptionPlain string `json:"descriptionPlain"`
	AdditionalPlain  string `json:"additionalPlain"`
	Lists            []struct {
		Text    string `json:"text"`
		Content string `json:"content"` // HTML
	} `json:"lists"`
	SalaryRange *struct {
		Min      int    `json:"min"`
		Max      int    `json:"max"`
		Currency string `json:"currency"`
		Interval string `json:"interval"` // "per-year-salary", ...
	} `json:"salaryRange"`
}

// ParseLever decodes a saved Lever postings response.
func ParseLever(r io.Reader, board, company string) ([]job.Job, error) {
	var postings []leverPosting
	if err := json.NewDecoder(r).Decode(&postings); err != nil {
		return nil, fmt.Errorf("lever: decode: %w", err)
	}
	return leverJobs(postings, board, company), nil
}

func leverJobs(postings []leverPosting, board, company string) []job.Job {
	if company == "" {
		company = board
	}
	jobs := make([]job.Job, 0, len(postings))
	for _, p := range postings {
		locs := append([]string{workplaceLabel(p.Workplace), p.Categories.Location}, p.Categories.AllLocations...)
		locs = append(locs, countryName(p.Country))

		desc := []string{p.DescriptionPlain}
		for _, l := range p.Lists {
			desc = append(desc, l.Text, htmlToText(l.Content))
		}
		desc = append(desc, p.AdditionalPlain)

		j := job.Job{
			Source:      "lever:" + board,
			ExternalID:  p.ID,
			Title:       strings.TrimSpace(p.Text),
			Company:     company,
			Location:    joinLocations(locs),
			Description: strings.TrimSpace(strings.Join(desc, "\n")),
			URL:         p.HostedURL,
		}
		if p.CreatedAt > 0 {
			j.PostedAt = time.UnixMilli(p.CreatedAt).UTC()
		}
		if s := p.SalaryRange; s != nil && s.Interval == "per-year-salary" {
			j.SalaryMin, j.SalaryMax, j.Currency = s.Min, s.Max, s.Currency
		}
		jobs = append(jobs, j)
	}
	return jobs
}
