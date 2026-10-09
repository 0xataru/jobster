package fetch

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/0xataru/jobster/internal/job"
)

// Personio fetches one company's public Personio job feed.
type Personio struct {
	Client  *Client
	Board   string // subdomain, e.g. "acme" from acme.jobs.personio.de
	Company string // display name; defaults to the feed's subcompany
	BaseURL string // defaults to https://{Board}.jobs.personio.de
}

func (p *Personio) Name() string { return "personio:" + p.Board }

func (p *Personio) base() string {
	if p.BaseURL != "" {
		return p.BaseURL
	}
	return "https://" + p.Board + ".jobs.personio.de"
}

func (p *Personio) Fetch(ctx context.Context) ([]job.Job, error) {
	body, err := p.Client.get(ctx, p.base()+"/xml")
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return ParsePersonio(body, p.Board, p.Company, p.base())
}

type personioFeed struct {
	XMLName   xml.Name
	Positions []struct {
		ID                string   `xml:"id"`
		Subcompany        string   `xml:"subcompany"`
		Office            string   `xml:"office"`
		AdditionalOffices []string `xml:"additionalOffices>office"`
		Name              string   `xml:"name"`
		Descriptions      []struct {
			Name  string `xml:"name"`
			Value string `xml:"value"` // HTML
		} `xml:"jobDescriptions>jobDescription"`
		CreatedAt string `xml:"createdAt"`
	} `xml:"position"`
}

// ParsePersonio decodes a saved Personio XML feed; base is the board's
// root URL, used to build job links.
func ParsePersonio(r io.Reader, board, company, base string) ([]job.Job, error) {
	var feed personioFeed
	if err := xml.NewDecoder(r).Decode(&feed); err != nil || feed.XMLName.Local != "workzag-jobs" {
		if err == nil {
			err = fmt.Errorf("root element is <%s>", feed.XMLName.Local)
		}
		return nil, fmt.Errorf("personio: not a job feed: %w", err)
	}
	jobs := make([]job.Job, 0, len(feed.Positions))
	for _, p := range feed.Positions {
		name := company
		if name == "" {
			name = strings.TrimSpace(p.Subcompany)
		}
		if name == "" {
			name = board
		}
		var desc []string
		for _, d := range p.Descriptions {
			desc = append(desc, d.Name, htmlToText(d.Value))
		}
		j := job.Job{
			Source:      "personio:" + board,
			ExternalID:  strings.TrimSpace(p.ID),
			Title:       strings.TrimSpace(p.Name),
			Company:     name,
			Location:    joinLocations(append([]string{p.Office}, p.AdditionalOffices...)),
			Description: strings.TrimSpace(strings.Join(desc, "\n")),
			URL:         strings.TrimRight(base, "/") + "/job/" + strings.TrimSpace(p.ID),
		}
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(p.CreatedAt)); err == nil {
			j.PostedAt = t
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}
