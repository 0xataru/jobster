package fetch

import (
	"context"
	"io"
	"net/url"
	"strings"

	"github.com/0xataru/jobster/internal/job"
)

// Djinni fetches the djinni.co RSS feed for one primary keyword ("Golang",
// "Rust", ...). The feed carries neither company nor location, so jobs come
// with both empty: filter by location with Params (e.g. employment=remote),
// and dedupe falls back to the URL.
type Djinni struct {
	Client  *Client
	Keyword string            // Djinni primary_keyword, e.g. "Golang"
	Params  map[string]string // extra filters as on djinni.co/jobs, e.g. {"employment": "remote"}
	BaseURL string            // defaults to https://djinni.co
}

func (d *Djinni) Name() string { return "djinni:" + strings.ToLower(d.Keyword) }

func (d *Djinni) Fetch(ctx context.Context) ([]job.Job, error) {
	base := d.BaseURL
	if base == "" {
		base = "https://djinni.co"
	}
	q := url.Values{"primary_keyword": {d.Keyword}}
	for k, v := range d.Params {
		q.Set(k, v)
	}
	body, err := d.Client.get(ctx, base+"/jobs/rss/?"+q.Encode())
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return ParseDjinni(body, d.Name())
}

type djinniItem struct {
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	Description string   `xml:"description"`
	PubDate     string   `xml:"pubDate"`
	GUID        string   `xml:"guid"`
	Categories  []string `xml:"category"`
}

// ParseDjinni decodes a saved Djinni RSS feed; source names the fetcher.
func ParseDjinni(r io.Reader, source string) ([]job.Job, error) {
	feed, err := decodeRSS[djinniItem](r)
	if err != nil {
		return nil, err
	}
	jobs := make([]job.Job, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		var tags []string
		for _, c := range it.Categories {
			if c = strings.TrimSpace(c); c != "" {
				tags = append(tags, c)
			}
		}
		jobs = append(jobs, job.Job{
			Source:      source,
			ExternalID:  strings.TrimSpace(it.GUID),
			Title:       strings.TrimSpace(it.Title),
			Description: htmlToText(it.Description),
			URL:         strings.TrimSpace(it.Link),
			PostedAt:    rssTime(it.PubDate),
			Tags:        tags,
		})
	}
	return jobs, nil
}
