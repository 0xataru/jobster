package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/0xataru/jobster/internal/job"
)

// Remotive fetches the software-development jobs from Remotive's public API.
// Its terms ask to link back to Remotive, so Job.URL is the Remotive page.
// The free API returns only a small sample of current jobs.
type Remotive struct {
	Client  *Client
	BaseURL string // defaults to https://remotive.com
}

func (r *Remotive) Name() string { return "remotive" }

func (r *Remotive) Fetch(ctx context.Context) ([]job.Job, error) {
	base := r.BaseURL
	if base == "" {
		base = "https://remotive.com"
	}
	var resp remotiveResponse
	if err := r.Client.getJSON(ctx, base+"/api/remote-jobs?category=software-dev", &resp); err != nil {
		return nil, err
	}
	return resp.jobs(), nil
}

type remotiveResponse struct {
	Jobs []struct {
		ID                        json.Number `json:"id"`
		URL                       string      `json:"url"`
		Title                     string      `json:"title"`
		CompanyName               string      `json:"company_name"`
		Tags                      []string    `json:"tags"`
		PublicationDate           string      `json:"publication_date"` // UTC, no zone suffix
		CandidateRequiredLocation string      `json:"candidate_required_location"`
		Salary                    string      `json:"salary"` // free text
		Description               string      `json:"description"`
	} `json:"jobs"`
}

// ParseRemotive decodes a saved Remotive API response.
func ParseRemotive(r io.Reader) ([]job.Job, error) {
	var resp remotiveResponse
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, fmt.Errorf("remotive: decode: %w", err)
	}
	return resp.jobs(), nil
}

func (resp *remotiveResponse) jobs() []job.Job {
	jobs := make([]job.Job, 0, len(resp.Jobs))
	for _, rj := range resp.Jobs {
		j := job.Job{
			Source:      "remotive",
			ExternalID:  rj.ID.String(),
			Title:       strings.TrimSpace(rj.Title),
			Company:     strings.TrimSpace(rj.CompanyName),
			Location:    strings.TrimSpace(rj.CandidateRequiredLocation),
			Description: htmlToText(rj.Description),
			URL:         rj.URL,
			Tags:        rj.Tags,
		}
		if t, err := time.Parse("2006-01-02T15:04:05", rj.PublicationDate); err == nil {
			j.PostedAt = t
		}
		if lo, hi, cur, ok := parseSalary(rj.Salary); ok {
			j.SalaryMin, j.SalaryMax, j.Currency = lo, hi, cur
		}
		jobs = append(jobs, j)
	}
	return jobs
}
