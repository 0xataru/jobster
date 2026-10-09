package fetch

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/0xataru/jobster/internal/job"
)

// Teamtailor fetches a company's Teamtailor career site RSS feed. Sites
// moved to a custom domain usually redirect the feed to an HTML page; that
// surfaces as a decode error for this source only.
type Teamtailor struct {
	Client  *Client
	Board   string // subdomain of teamtailor.com, e.g. "career"
	Company string // display name; defaults to the feed title
	BaseURL string // defaults to https://{Board}.teamtailor.com
}

func (t *Teamtailor) Name() string { return "teamtailor:" + t.Board }

func (t *Teamtailor) Fetch(ctx context.Context) ([]job.Job, error) {
	base := t.BaseURL
	if base == "" {
		base = "https://" + t.Board + ".teamtailor.com"
	}
	body, err := t.Client.get(ctx, base+"/jobs.rss")
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return ParseTeamtailor(body, t.Board, t.Company)
}

type teamtailorItem struct {
	Title        string `xml:"title"`
	Description  string `xml:"description"` // HTML, escaped once by XML
	PubDate      string `xml:"pubDate"`
	Link         string `xml:"link"`
	GUID         string `xml:"guid"`
	RemoteStatus string `xml:"remoteStatus"` // none | hybrid | fully | temporary
	Locations    []struct {
		Name    string `xml:"name"`
		City    string `xml:"city"`
		Country string `xml:"country"`
	} `xml:"https://teamtailor.com/locations locations>location"`
}

// ParseTeamtailor decodes a saved Teamtailor jobs.rss feed.
func ParseTeamtailor(r io.Reader, board, company string) ([]job.Job, error) {
	feed, err := decodeRSS[teamtailorItem](r)
	if err != nil {
		return nil, fmt.Errorf("teamtailor (custom domain?): %w", err)
	}
	if company == "" {
		company = strings.TrimSpace(feed.Channel.Title)
	}
	jobs := make([]job.Job, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		locs := []string{workplaceLabel(it.RemoteStatus)}
		for _, l := range it.Locations {
			locs = append(locs, l.Name, l.City, l.Country)
		}
		jobs = append(jobs, job.Job{
			Source:      "teamtailor:" + board,
			ExternalID:  it.GUID,
			Title:       strings.TrimSpace(it.Title),
			Company:     company,
			Location:    joinLocations(locs),
			Description: htmlToText(it.Description),
			URL:         strings.TrimSpace(it.Link),
			PostedAt:    rssTime(it.PubDate),
		})
	}
	return jobs, nil
}
