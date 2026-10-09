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

// Workable fetches one company's public Workable job board.
type Workable struct {
	Client  *Client
	Board   string // account name, e.g. "acme" from apply.workable.com/acme
	Company string // display name; defaults to the account's name
	BaseURL string // defaults to https://apply.workable.com
}

func (w *Workable) Name() string { return "workable:" + w.Board }

func (w *Workable) Fetch(ctx context.Context) ([]job.Job, error) {
	base := w.BaseURL
	if base == "" {
		base = "https://apply.workable.com"
	}
	var resp workableResponse
	u := fmt.Sprintf("%s/api/v1/widget/accounts/%s?details=true", base, url.PathEscape(w.Board))
	if err := w.Client.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	return resp.jobs(w.Board, w.Company), nil
}

type workableResponse struct {
	Name string `json:"name"`
	Jobs []struct {
		Title         string `json:"title"`
		Shortcode     string `json:"shortcode"`
		URL           string `json:"url"`
		Telecommuting bool   `json:"telecommuting"`
		PublishedOn   string `json:"published_on"` // 2006-01-02
		Country       string `json:"country"`
		City          string `json:"city"`
		Locations     []struct {
			Country string `json:"country"`
			City    string `json:"city"`
		} `json:"locations"`
		Description string `json:"description"` // HTML; present with details=true
	} `json:"jobs"`
}

// ParseWorkable decodes a saved Workable widget response.
func ParseWorkable(r io.Reader, board, company string) ([]job.Job, error) {
	var resp workableResponse
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, fmt.Errorf("workable: decode: %w", err)
	}
	return resp.jobs(board, company), nil
}

func (resp *workableResponse) jobs(board, company string) []job.Job {
	if company == "" {
		company = strings.TrimSpace(resp.Name)
	}
	if company == "" {
		company = board
	}
	jobs := make([]job.Job, 0, len(resp.Jobs))
	for _, wj := range resp.Jobs {
		locs := []string{wj.City, wj.Country}
		if wj.Telecommuting {
			locs = append([]string{"Remote"}, locs...)
		}
		for _, l := range wj.Locations {
			locs = append(locs, l.City, l.Country)
		}
		j := job.Job{
			Source:      "workable:" + board,
			ExternalID:  wj.Shortcode,
			Title:       strings.TrimSpace(wj.Title),
			Company:     company,
			Location:    joinLocations(locs),
			Description: htmlToText(wj.Description),
			URL:         wj.URL,
		}
		if t, err := time.Parse(time.DateOnly, wj.PublishedOn); err == nil {
			j.PostedAt = t
		}
		jobs = append(jobs, j)
	}
	return jobs
}
