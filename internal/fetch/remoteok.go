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

// RemoteOK fetches the public RemoteOK feed. Its API terms require linking
// back to the RemoteOK posting, so Job.URL is the RemoteOK page.
type RemoteOK struct {
	Client  *Client
	BaseURL string // defaults to https://remoteok.com
}

func (r *RemoteOK) Name() string { return "remoteok" }

func (r *RemoteOK) Fetch(ctx context.Context) ([]job.Job, error) {
	base := r.BaseURL
	if base == "" {
		base = "https://remoteok.com"
	}
	var raw []json.RawMessage
	if err := r.Client.getJSON(ctx, base+"/api", &raw); err != nil {
		return nil, err
	}
	return parseRemoteOK(raw)
}

type remoteOKJob struct {
	ID          json.RawMessage `json:"id"` // string in practice, number historically
	Epoch       int64           `json:"epoch"`
	Date        string          `json:"date"`
	Company     string          `json:"company"`
	Position    string          `json:"position"`
	Tags        []string        `json:"tags"`
	Description string          `json:"description"`
	Location    string          `json:"location"`
	SalaryMin   int             `json:"salary_min"`
	SalaryMax   int             `json:"salary_max"`
	URL         string          `json:"url"`
}

// ParseRemoteOK decodes a saved RemoteOK API response.
func ParseRemoteOK(r io.Reader) ([]job.Job, error) {
	var raw []json.RawMessage
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("remoteok: decode: %w", err)
	}
	return parseRemoteOK(raw)
}

func parseRemoteOK(raw []json.RawMessage) ([]job.Job, error) {
	jobs := make([]job.Job, 0, len(raw))
	for i, msg := range raw {
		var rj remoteOKJob
		if err := json.Unmarshal(msg, &rj); err != nil {
			return nil, fmt.Errorf("remoteok: item %d: %w", i, err)
		}
		// The first element is a legal notice, not a job.
		if rj.Position == "" || rj.URL == "" {
			continue
		}
		j := job.Job{
			Source:      "remoteok",
			ExternalID:  strings.Trim(string(rj.ID), `"`),
			Title:       strings.TrimSpace(fixMojibake(rj.Position)),
			Company:     strings.TrimSpace(fixMojibake(rj.Company)),
			Location:    strings.TrimSpace(fixMojibake(rj.Location)),
			Description: htmlToText(fixMojibake(rj.Description)),
			URL:         rj.URL,
			Tags:        rj.Tags,
			SalaryMin:   rj.SalaryMin,
			SalaryMax:   rj.SalaryMax,
		}
		if t, err := time.Parse(time.RFC3339, rj.Date); err == nil {
			j.PostedAt = t
		} else if rj.Epoch > 0 {
			j.PostedAt = time.Unix(rj.Epoch, 0)
		}
		if j.SalaryMin > 0 || j.SalaryMax > 0 {
			j.Currency = "USD"
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}
