package fetch

import (
	"context"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/0xataru/jobster/internal/job"
)

// WeWorkRemotely fetches one We Work Remotely category RSS feed, e.g.
// "remote-back-end-programming-jobs".
type WeWorkRemotely struct {
	Client   *Client
	Category string
	BaseURL  string // defaults to https://weworkremotely.com
}

func (w *WeWorkRemotely) Name() string { return "wwr:" + w.Category }

func (w *WeWorkRemotely) Fetch(ctx context.Context) ([]job.Job, error) {
	base := w.BaseURL
	if base == "" {
		base = "https://weworkremotely.com"
	}
	body, err := w.Client.get(ctx, base+"/categories/"+url.PathEscape(w.Category)+".rss")
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return ParseWeWorkRemotely(body, w.Name())
}

type wwrItem struct {
	Title       string `xml:"title"` // "Company: Role"
	Region      string `xml:"region"`
	Country     string `xml:"country"`
	Skills      string `xml:"skills"` // "Java, PostgreSQL, and Spring"
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	GUID        string `xml:"guid"`
	Link        string `xml:"link"`
}

var wwrSkillSep = regexp.MustCompile(`\s*,\s*(?:and\s+)?|\s+and\s+`)

// ParseWeWorkRemotely decodes a saved WWR category feed; source names the
// fetcher.
func ParseWeWorkRemotely(r io.Reader, source string) ([]job.Job, error) {
	feed, err := decodeRSS[wwrItem](r)
	if err != nil {
		return nil, err
	}
	jobs := make([]job.Job, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		company, title, ok := strings.Cut(it.Title, ": ")
		if !ok {
			company, title = "", it.Title
		}
		var tags []string
		for _, s := range wwrSkillSep.Split(it.Skills, -1) {
			if s = strings.TrimSpace(s); s != "" {
				tags = append(tags, s)
			}
		}
		jobs = append(jobs, job.Job{
			Source:      source,
			ExternalID:  strings.TrimSpace(it.GUID),
			Title:       strings.TrimSpace(title),
			Company:     strings.TrimSpace(company),
			Location:    joinLocations([]string{it.Region, it.Country}),
			Description: htmlToText(it.Description),
			URL:         strings.TrimSpace(it.Link),
			PostedAt:    rssTime(it.PubDate),
			Tags:        tags,
		})
	}
	return jobs, nil
}
