// Package notify delivers the digest of new matching jobs.
package notify

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/0xataru/jobster/internal/job"
)

// Notifier delivers a digest. An error means nothing should be marked as
// notified, so the jobs are retried on the next run.
type Notifier interface {
	Notify(ctx context.Context, jobs []job.Scored) error
}

// Writer prints the digest as plain text, for local runs and dry runs.
type Writer struct {
	W io.Writer
}

func (w Writer) Notify(_ context.Context, jobs []job.Scored) error {
	if len(jobs) == 0 {
		_, err := fmt.Fprintln(w.W, "No new matching jobs.")
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d new matching jobs\n\n", len(jobs))
	for _, j := range jobs {
		if j.Company != "" {
			fmt.Fprintf(&b, "[%d] %s — %s\n", j.Score, j.Title, j.Company)
		} else {
			fmt.Fprintf(&b, "[%d] %s\n", j.Score, j.Title)
		}
		var meta []string
		if j.Location != "" && j.Location != j.Title { // HN posts use the header for both
			meta = append(meta, j.Location)
		}
		if s := Salary(j.Job); s != "" {
			meta = append(meta, s)
		}
		if !j.PostedAt.IsZero() {
			meta = append(meta, "posted "+j.PostedAt.Format("2006-01-02"))
		}
		meta = append(meta, "via "+j.Source)
		fmt.Fprintf(&b, "    %s\n    %s\n", strings.Join(meta, " · "), j.URL)
		if len(j.Reasons) > 0 {
			fmt.Fprintf(&b, "    (%s)\n", strings.Join(j.Reasons, ", "))
		}
		b.WriteString("\n")
	}
	_, err := io.WriteString(w.W, b.String())
	return err
}

// Salary formats a job's salary range, or "" when unknown.
func Salary(j job.Job) string {
	k := func(n int) string { return fmt.Sprintf("%dk", n/1000) }
	var s string
	switch {
	case j.SalaryMin > 0 && j.SalaryMax > 0 && j.SalaryMin != j.SalaryMax:
		s = k(j.SalaryMin) + "–" + k(j.SalaryMax)
	case j.SalaryMax > 0:
		s = k(j.SalaryMax)
	case j.SalaryMin > 0:
		s = k(j.SalaryMin) + "+"
	default:
		return ""
	}
	return strings.TrimSpace(s + " " + j.Currency)
}
