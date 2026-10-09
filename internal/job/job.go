// Package job defines the normalized job posting shared by every pipeline stage.
package job

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
	"unicode"
)

// Job is a posting normalized from any source.
type Job struct {
	Source      string // fetcher name, e.g. "remoteok" or "greenhouse:cloudflare"
	ExternalID  string // source-specific identifier
	Title       string
	Company     string
	Location    string // free text as published; empty when unspecified
	Description string // plain text, HTML stripped
	URL         string
	PostedAt    time.Time // zero when unknown
	SalaryMin   int       // annual; 0 when unknown
	SalaryMax   int       // annual; 0 when unknown
	Currency    string    // ISO 4217; empty when unknown
	Tags        []string
}

// Scored is a job together with its score and the rules that produced it.
type Scored struct {
	Job
	Score   int
	Reasons []string
}

// Key identifies a posting across sources: the same role at the same company
// listed on RemoteOK and on the company's ATS yields the same key. Sources
// that hide the company (Djinni) are keyed by URL, so anonymous postings with
// the same title stay distinct.
func (j Job) Key() string {
	id := "url|" + j.URL
	if company := NormalizeCompany(j.Company); company != "" {
		id = company + "|" + normalize(j.Title)
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:12])
}

var companySuffixes = map[string]bool{
	"inc": true, "llc": true, "ltd": true, "limited": true, "gmbh": true,
	"ag": true, "sa": true, "sl": true, "bv": true, "corp": true, "co": true,
}

// NormalizeCompany lowercases a company name and drops punctuation and legal
// suffixes, so "Cloudflare, Inc." and "cloudflare" compare equal.
func NormalizeCompany(name string) string {
	words := strings.Fields(normalize(name))
	for len(words) > 1 && companySuffixes[words[len(words)-1]] {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}

// normalize lowercases s and collapses every run of non-alphanumerics to a
// single space.
func normalize(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}
