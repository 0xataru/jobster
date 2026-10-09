package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/0xataru/jobster/internal/job"
)

// Greenhouse fetches one company's public Greenhouse job board.
type Greenhouse struct {
	Client  *Client
	Board   string // board token, e.g. "cloudflare"
	BaseURL string // defaults to https://boards-api.greenhouse.io
}

func (g *Greenhouse) Name() string { return "greenhouse:" + g.Board }

func (g *Greenhouse) Fetch(ctx context.Context) ([]job.Job, error) {
	base := g.BaseURL
	if base == "" {
		base = "https://boards-api.greenhouse.io"
	}
	u := fmt.Sprintf("%s/v1/boards/%s/jobs?content=true", base, url.PathEscape(g.Board))
	var resp greenhouseResponse
	if err := g.Client.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	return resp.jobs(g.Board), nil
}

type greenhouseResponse struct {
	Jobs []struct {
		ID             int64  `json:"id"`
		Title          string `json:"title"`
		AbsoluteURL    string `json:"absolute_url"`
		UpdatedAt      string `json:"updated_at"`
		FirstPublished string `json:"first_published"`
		CompanyName    string `json:"company_name"`
		Content        string `json:"content"` // HTML, itself HTML-escaped
		Location       struct {
			Name string `json:"name"`
		} `json:"location"`
		Offices []struct {
			Name     string `json:"name"`
			Location string `json:"location"`
		} `json:"offices"`
	} `json:"jobs"`
}

// ParseGreenhouse decodes a saved Greenhouse board response for board.
func ParseGreenhouse(r io.Reader, board string) ([]job.Job, error) {
	var resp greenhouseResponse
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, fmt.Errorf("greenhouse: decode: %w", err)
	}
	return resp.jobs(board), nil
}

func (resp *greenhouseResponse) jobs(board string) []job.Job {
	jobs := make([]job.Job, 0, len(resp.Jobs))
	for _, gj := range resp.Jobs {
		company := gj.CompanyName
		if company == "" {
			company = board
		}
		// The location name is often vague ("Remote", "Hybrid"); office
		// locations add the country or region the filters need.
		locs := []string{gj.Location.Name}
		for _, o := range gj.Offices {
			l := o.Location
			if l == "" {
				l = o.Name
			}
			if l != "" && !containsFold(locs, l) {
				locs = append(locs, l)
			}
		}
		j := job.Job{
			Source:      "greenhouse:" + board,
			ExternalID:  strconv.FormatInt(gj.ID, 10),
			Title:       strings.TrimSpace(gj.Title),
			Company:     company,
			Location:    strings.Trim(strings.Join(locs, "; "), "; "),
			Description: htmlToText(html.UnescapeString(gj.Content)),
			URL:         gj.AbsoluteURL,
		}
		for _, ts := range []string{gj.FirstPublished, gj.UpdatedAt} {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				j.PostedAt = t
				break
			}
		}
		jobs = append(jobs, j)
	}
	return jobs
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}
