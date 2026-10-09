package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/0xataru/jobster/internal/job"
)

const himalayasPageSize = 20 // fixed by the API

// Himalayas fetches the newest results of one Himalayas job search.
type Himalayas struct {
	Client  *Client
	Query   string // search terms, e.g. "golang"
	Pages   int    // pages of 20 results to read; defaults to 1
	BaseURL string // defaults to https://himalayas.app
}

func (h *Himalayas) Name() string { return "himalayas:" + h.Query }

func (h *Himalayas) Fetch(ctx context.Context) ([]job.Job, error) {
	base := h.BaseURL
	if base == "" {
		base = "https://himalayas.app"
	}
	var jobs []job.Job
	for page := 1; page <= max(h.Pages, 1); page++ {
		q := url.Values{"q": {h.Query}, "sort": {"recent"}, "page": {strconv.Itoa(page)}}
		var resp himalayasResponse
		if err := h.Client.getJSON(ctx, base+"/jobs/api/search?"+q.Encode(), &resp); err != nil {
			if page > 1 && len(jobs) > 0 {
				break // keep what earlier pages returned
			}
			return nil, err
		}
		jobs = append(jobs, resp.jobs(h.Name())...)
		// Pages can come back short (the API drops expired jobs after
		// paging), so only an empty page or the total marks the end.
		if len(resp.Jobs) == 0 || page*himalayasPageSize >= resp.TotalCount {
			break
		}
	}
	return jobs, nil
}

type himalayasResponse struct {
	TotalCount int `json:"totalCount"`
	Jobs       []struct {
		Title                string   `json:"title"`
		CompanyName          string   `json:"companyName"`
		MinSalary            *float64 `json:"minSalary"`
		MaxSalary            *float64 `json:"maxSalary"`
		SalaryPeriod         string   `json:"salaryPeriod"`
		Currency             string   `json:"currency"`
		Seniority            []string `json:"seniority"`
		LocationRestrictions []string `json:"locationRestrictions"` // empty: worldwide
		Categories           []string `json:"categories"`
		Description          string   `json:"description"`
		PubDate              int64    `json:"pubDate"` // unix seconds
		ApplicationLink      string   `json:"applicationLink"`
		GUID                 string   `json:"guid"`
	} `json:"jobs"`
}

// ParseHimalayas decodes a saved Himalayas search response; source names the
// fetcher.
func ParseHimalayas(r io.Reader, source string) ([]job.Job, error) {
	var resp himalayasResponse
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, fmt.Errorf("himalayas: decode: %w", err)
	}
	return resp.jobs(source), nil
}

func (resp *himalayasResponse) jobs(source string) []job.Job {
	jobs := make([]job.Job, 0, len(resp.Jobs))
	for _, hj := range resp.Jobs {
		loc := strings.Join(hj.LocationRestrictions, "; ")
		if loc == "" {
			loc = "Worldwide"
		}
		j := job.Job{
			Source:      source,
			ExternalID:  hj.GUID,
			Title:       strings.TrimSpace(hj.Title),
			Company:     strings.TrimSpace(hj.CompanyName),
			Location:    loc,
			Description: htmlToText(hj.Description),
			URL:         hj.ApplicationLink,
			Tags:        append(append([]string{}, hj.Seniority...), hj.Categories...),
		}
		if hj.PubDate > 0 {
			j.PostedAt = time.Unix(hj.PubDate, 0).UTC()
		}
		if hj.SalaryPeriod == "annual" {
			if hj.MinSalary != nil {
				j.SalaryMin = int(*hj.MinSalary)
			}
			if hj.MaxSalary != nil {
				j.SalaryMax = int(*hj.MaxSalary)
			}
			if j.SalaryMin > 0 || j.SalaryMax > 0 {
				j.Currency = hj.Currency
			}
		}
		jobs = append(jobs, j)
	}
	return jobs
}
