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

// Ashby fetches one company's public Ashby job board.
type Ashby struct {
	Client  *Client
	Board   string // job board name, e.g. "supabase"
	Company string // display name; Ashby's API does not return one
	BaseURL string // defaults to https://api.ashbyhq.com
}

func (a *Ashby) Name() string { return "ashby:" + a.Board }

func (a *Ashby) Fetch(ctx context.Context) ([]job.Job, error) {
	base := a.BaseURL
	if base == "" {
		base = "https://api.ashbyhq.com"
	}
	u := fmt.Sprintf("%s/posting-api/job-board/%s?includeCompensation=true", base, url.PathEscape(a.Board))
	var resp ashbyResponse
	if err := a.Client.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	return resp.jobs(a.Board, a.Company), nil
}

type ashbyLocation struct {
	Location string `json:"location"`
	Address  struct {
		PostalAddress struct {
			AddressCountry string `json:"addressCountry"`
		} `json:"postalAddress"`
	} `json:"address"`
}

type ashbyResponse struct {
	Jobs []struct {
		ID                 string          `json:"id"`
		Title              string          `json:"title"`
		Location           string          `json:"location"`
		SecondaryLocations []ashbyLocation `json:"secondaryLocations"`
		Address            struct {
			PostalAddress struct {
				AddressCountry string `json:"addressCountry"`
			} `json:"postalAddress"`
		} `json:"address"`
		IsListed         bool   `json:"isListed"`
		IsRemote         bool   `json:"isRemote"`
		WorkplaceType    string `json:"workplaceType"` // Remote | Hybrid | OnSite
		PublishedAt      string `json:"publishedAt"`
		JobURL           string `json:"jobUrl"`
		DescriptionPlain string `json:"descriptionPlain"`
		Compensation     *struct {
			SummaryComponents []struct {
				CompensationType string   `json:"compensationType"` // Salary, EquityPercentage, ...
				Interval         string   `json:"interval"`         // "1 YEAR", ...
				CurrencyCode     string   `json:"currencyCode"`
				MinValue         *float64 `json:"minValue"`
				MaxValue         *float64 `json:"maxValue"`
			} `json:"summaryComponents"`
		} `json:"compensation"`
	} `json:"jobs"`
}

// ParseAshby decodes a saved Ashby job board response.
func ParseAshby(r io.Reader, board, company string) ([]job.Job, error) {
	var resp ashbyResponse
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, fmt.Errorf("ashby: decode: %w", err)
	}
	return resp.jobs(board, company), nil
}

func (resp *ashbyResponse) jobs(board, company string) []job.Job {
	if company == "" {
		company = board
	}
	jobs := make([]job.Job, 0, len(resp.Jobs))
	for _, aj := range resp.Jobs {
		if !aj.IsListed {
			continue
		}
		locs := []string{workplaceLabel(aj.WorkplaceType), aj.Location, aj.Address.PostalAddress.AddressCountry}
		for _, sl := range aj.SecondaryLocations {
			locs = append(locs, sl.Location, sl.Address.PostalAddress.AddressCountry)
		}
		j := job.Job{
			Source:      "ashby:" + board,
			ExternalID:  aj.ID,
			Title:       strings.TrimSpace(aj.Title),
			Company:     company,
			Location:    joinLocations(locs),
			Description: strings.TrimSpace(aj.DescriptionPlain),
			URL:         aj.JobURL,
		}
		if t, err := time.Parse(time.RFC3339, aj.PublishedAt); err == nil {
			j.PostedAt = t
		}
		if aj.Compensation != nil {
			for _, c := range aj.Compensation.SummaryComponents {
				if c.CompensationType == "Salary" && c.Interval == "1 YEAR" {
					if c.MinValue != nil {
						j.SalaryMin = int(*c.MinValue)
					}
					if c.MaxValue != nil {
						j.SalaryMax = int(*c.MaxValue)
					}
					j.Currency = c.CurrencyCode
					break
				}
			}
		}
		jobs = append(jobs, j)
	}
	return jobs
}

// workplaceLabel normalizes ATS workplace types ("OnSite", "onsite",
// "hybrid", "fully", ...) into the words the region filters match on.
func workplaceLabel(t string) string {
	switch strings.ToLower(strings.ReplaceAll(t, "_", "")) {
	case "remote", "fully":
		return "Remote"
	case "hybrid":
		return "Hybrid"
	case "onsite", "none":
		return "On-site"
	}
	return ""
}

// joinLocations joins the non-empty, case-insensitively distinct parts.
func joinLocations(parts []string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" && !containsFold(out, p) {
			out = append(out, p)
		}
	}
	return strings.Join(out, "; ")
}
