package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/0xataru/jobster/internal/job"
)

// Recruitee fetches one company's public Recruitee offers.
type Recruitee struct {
	Client  *Client
	Board   string // subdomain of recruitee.com, e.g. "acme"
	Company string // display name; defaults to the offers' company name
	BaseURL string // defaults to https://{Board}.recruitee.com
}

func (r *Recruitee) Name() string { return "recruitee:" + r.Board }

func (r *Recruitee) Fetch(ctx context.Context) ([]job.Job, error) {
	base := r.BaseURL
	if base == "" {
		base = "https://" + r.Board + ".recruitee.com"
	}
	var resp recruiteeResponse
	if err := r.Client.getJSON(ctx, base+"/api/offers/", &resp); err != nil {
		return nil, err
	}
	return resp.jobs(r.Board, r.Company), nil
}

type recruiteeResponse struct {
	Offers []struct {
		ID           int64  `json:"id"`
		Title        string `json:"title"`
		CareersURL   string `json:"careers_url"`
		CompanyName  string `json:"company_name"`
		Location     string `json:"location"`
		Country      string `json:"country"`
		Remote       bool   `json:"remote"`
		PublishedAt  string `json:"published_at"` // "2006-01-02 15:04:05 UTC"
		Description  string `json:"description"`  // HTML
		Requirements string `json:"requirements"` // HTML
		Salary       struct {
			Min      json.RawMessage `json:"min"` // number or numeric string
			Max      json.RawMessage `json:"max"`
			Period   string          `json:"period"` // year | month | ...
			Currency string          `json:"currency"`
		} `json:"salary"`
	} `json:"offers"`
}

// ParseRecruitee decodes a saved Recruitee offers response.
func ParseRecruitee(r io.Reader, board, company string) ([]job.Job, error) {
	var resp recruiteeResponse
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, fmt.Errorf("recruitee: decode: %w", err)
	}
	return resp.jobs(board, company), nil
}

func (resp *recruiteeResponse) jobs(board, company string) []job.Job {
	jobs := make([]job.Job, 0, len(resp.Offers))
	for _, o := range resp.Offers {
		name := company
		if name == "" {
			name = strings.TrimSpace(o.CompanyName)
		}
		if name == "" {
			name = board
		}
		locs := []string{o.Location, o.Country}
		if o.Remote {
			locs = append([]string{"Remote"}, locs...)
		}
		j := job.Job{
			Source:      "recruitee:" + board,
			ExternalID:  strconv.FormatInt(o.ID, 10),
			Title:       strings.TrimSpace(o.Title),
			Company:     name,
			Location:    joinLocations(locs),
			Description: strings.TrimSpace(htmlToText(o.Description) + "\n" + htmlToText(o.Requirements)),
			URL:         o.CareersURL,
		}
		if t, err := time.Parse("2006-01-02 15:04:05 MST", o.PublishedAt); err == nil {
			j.PostedAt = t
		}
		if o.Salary.Period == "year" {
			j.SalaryMin, j.SalaryMax = flexInt(o.Salary.Min), flexInt(o.Salary.Max)
			if j.SalaryMin > 0 || j.SalaryMax > 0 {
				j.Currency = o.Salary.Currency
			}
		}
		jobs = append(jobs, j)
	}
	return jobs
}

// flexInt reads a JSON number or numeric string, returning 0 otherwise.
func flexInt(raw json.RawMessage) int {
	s := strings.Trim(string(raw), `"`)
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int(f)
}
