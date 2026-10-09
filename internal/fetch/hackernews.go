package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/0xataru/jobster/internal/job"
)

// HackerNews fetches the latest monthly "Ask HN: Who is hiring?" thread via
// the Algolia HN API. Each top-level comment becomes one job.
//
// Posts follow the convention "Company | Role | Location | Remote | ..." (some
// use "—" instead of "|") on their first line, but the fields are free text in
// no fixed order. So the first line is used both as Title and Location, and
// the first field as Company; title and region filters then match against the
// whole header.
type HackerNews struct {
	Client  *Client
	BaseURL string // defaults to https://hn.algolia.com
}

func (h *HackerNews) Name() string { return "hn" }

func (h *HackerNews) Fetch(ctx context.Context) ([]job.Job, error) {
	base := h.BaseURL
	if base == "" {
		base = "https://hn.algolia.com"
	}
	var search struct {
		Hits []struct {
			ObjectID string `json:"objectID"`
			Title    string `json:"title"`
		} `json:"hits"`
	}
	u := base + "/api/v1/search_by_date?tags=story,author_whoishiring&hitsPerPage=10"
	if err := h.Client.getJSON(ctx, u, &search); err != nil {
		return nil, err
	}
	threadID := ""
	for _, hit := range search.Hits {
		if strings.HasPrefix(strings.ToLower(hit.Title), "ask hn: who is hiring") {
			threadID = hit.ObjectID
			break
		}
	}
	if threadID == "" {
		return nil, fmt.Errorf("no \"Who is hiring?\" thread among the latest whoishiring posts")
	}

	var thread hnItem
	if err := h.Client.getJSON(ctx, base+"/api/v1/items/"+threadID, &thread); err != nil {
		return nil, err
	}
	return thread.jobs(), nil
}

type hnItem struct {
	ID        int64    `json:"id"`
	Author    string   `json:"author"`
	CreatedAt string   `json:"created_at"`
	Text      string   `json:"text"` // HTML
	Children  []hnItem `json:"children"`
}

// ParseHackerNews decodes a saved Algolia item response for a hiring thread.
func ParseHackerNews(r io.Reader) ([]job.Job, error) {
	var thread hnItem
	if err := json.NewDecoder(r).Decode(&thread); err != nil {
		return nil, fmt.Errorf("hn: decode: %w", err)
	}
	return thread.jobs(), nil
}

var (
	hnParagraph = regexp.MustCompile(`(?i)<p>`)
	hnFieldSep  = regexp.MustCompile(`\s*(?:\||—|–)\s*`)
	hnURL       = regexp.MustCompile(`https?://\S+`)
)

func (thread *hnItem) jobs() []job.Job {
	jobs := make([]job.Job, 0, len(thread.Children))
	for _, c := range thread.Children {
		if c.Text == "" { // deleted or flagged
			continue
		}
		header := htmlToText(hnParagraph.Split(c.Text, 2)[0])
		header = strings.Join(strings.Fields(header), " ")
		fields := hnFieldSep.Split(header, 2)
		if len(fields) < 2 {
			continue // not following the posting format; usually a meta comment
		}
		company := strings.Join(strings.Fields(hnURL.ReplaceAllString(fields[0], "")), " ")
		j := job.Job{
			Source:      "hn",
			ExternalID:  strconv.FormatInt(c.ID, 10),
			Title:       header,
			Company:     company,
			Location:    header,
			Description: htmlToText(hnParagraph.ReplaceAllString(c.Text, "\n")),
			URL:         "https://news.ycombinator.com/item?id=" + strconv.FormatInt(c.ID, 10),
		}
		if t, err := time.Parse(time.RFC3339, c.CreatedAt); err == nil {
			j.PostedAt = t
		}
		jobs = append(jobs, j)
	}
	return jobs
}
