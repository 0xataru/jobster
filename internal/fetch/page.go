package fetch

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/0xataru/jobster/internal/job"
)

// page is what the careers fallback reads from an HTML document.
type page struct {
	Title       string // <title>
	OGTitle     string
	Description string // og:description or meta description
	Text        string // visible text, minus scripts, navigation and footers
	Links       []pageLink
	JSONLD      []string // raw application/ld+json blocks
}

type pageLink struct {
	URL  *url.URL
	Text string
}

// Elements whose text is never job content.
var skipText = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Svg: true,
	atom.Nav: true, atom.Header: true, atom.Footer: true, atom.Template: true,
}

var blockElems = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Li: true, atom.Br: true, atom.Section: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Tr: true, atom.Article: true, atom.Ul: true, atom.Ol: true,
}

// parsePage extracts metadata, links (resolved against base) and text.
func parsePage(doc string, base *url.URL) page {
	var p page
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return p
	}
	var text strings.Builder
	var walk func(n *html.Node, visible bool)
	walk = func(n *html.Node, visible bool) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Title:
				if p.Title == "" {
					p.Title = strings.TrimSpace(nodeText(n))
				}
			case atom.Meta:
				key := strings.ToLower(attr(n, "property") + attr(n, "name"))
				switch key {
				case "og:title":
					p.OGTitle = strings.TrimSpace(attr(n, "content"))
				case "og:description", "description":
					if p.Description == "" {
						p.Description = strings.TrimSpace(attr(n, "content"))
					}
				}
			case atom.Script:
				if strings.EqualFold(attr(n, "type"), "application/ld+json") {
					p.JSONLD = append(p.JSONLD, nodeText(n))
				}
			case atom.A:
				if href := strings.TrimSpace(attr(n, "href")); href != "" && base != nil {
					if u, err := base.Parse(href); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
						u.Fragment = ""
						p.Links = append(p.Links, pageLink{URL: u, Text: strings.Join(strings.Fields(nodeText(n)), " ")})
					}
				}
			}
			if skipText[n.DataAtom] {
				visible = false
			}
			if blockElems[n.DataAtom] {
				text.WriteString("\n")
			}
		}
		if n.Type == html.TextNode && visible {
			text.WriteString(n.Data)
			text.WriteString(" ")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, visible)
		}
	}
	walk(root, true)
	p.Text = htmlToText(html.EscapeString(text.String()))
	return p
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

// nodeText returns the text inside n. Nested scripts and styles are skipped:
// CSS-in-JS sites put <style> blocks inside links and headings.
func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && nonContent[c.DataAtom] {
				continue
			}
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

var nonContent = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Svg: true, atom.Template: true,
}

// jobPostings extracts schema.org JobPosting objects from JSON-LD blocks,
// including ones nested in arrays or an @graph.
func jobPostings(blocks []string) []map[string]any {
	var out []map[string]any
	var visit func(v any)
	visit = func(v any) {
		switch t := v.(type) {
		case []any:
			for _, e := range t {
				visit(e)
			}
		case map[string]any:
			if isType(t["@type"], "JobPosting") {
				out = append(out, t)
				return
			}
			visit(t["@graph"])
			visit(t["itemListElement"])
			visit(t["item"])
		}
	}
	for _, b := range blocks {
		var v any
		if json.Unmarshal([]byte(b), &v) == nil {
			visit(v)
		}
	}
	return out
}

func isType(v any, want string) bool {
	switch t := v.(type) {
	case string:
		return t == want
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

// ldString reads a JSON-LD value that may be a string, a number, an object
// with "name", or a list of those.
func ldString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case map[string]any:
		return ldString(t["name"])
	case []any:
		var parts []string
		for _, e := range t {
			if s := ldString(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "; ")
	}
	return ""
}

func ldList(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	if v == nil {
		return nil
	}
	return []any{v}
}

// jobFromPosting converts a JSON-LD JobPosting. pageURL is used when the
// posting has no url of its own.
func jobFromPosting(p map[string]any, source, company, pageURL string) job.Job {
	j := job.Job{
		Source:      source,
		Title:       ldString(p["title"]),
		Company:     company,
		Description: htmlToText(ldString(p["description"])),
		URL:         ldString(p["url"]),
	}
	if j.URL == "" {
		j.URL = pageURL
	}
	j.ExternalID = j.URL
	if j.Company == "" {
		j.Company = ldString(p["hiringOrganization"])
	}

	var locs []string
	if strings.EqualFold(ldString(p["jobLocationType"]), "TELECOMMUTE") {
		locs = append(locs, "Remote")
	}
	for _, l := range ldList(p["jobLocation"]) {
		lm, _ := l.(map[string]any)
		switch addr := lm["address"].(type) {
		case map[string]any:
			locs = append(locs, ldString(addr["addressLocality"]), ldString(addr["addressRegion"]), countryName(ldString(addr["addressCountry"])))
		default:
			locs = append(locs, ldString(addr))
		}
	}
	for _, req := range ldList(p["applicantLocationRequirements"]) {
		locs = append(locs, countryName(ldString(req)))
	}
	j.Location = joinLocations(locs)

	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", time.DateOnly} {
		if t, err := time.Parse(layout, ldString(p["datePosted"])); err == nil {
			j.PostedAt = t
			break
		}
	}

	if sal, ok := p["baseSalary"].(map[string]any); ok {
		if val, ok := sal["value"].(map[string]any); ok && strings.EqualFold(ldString(val["unitText"]), "YEAR") {
			lo, _ := val["minValue"].(float64)
			hi, _ := val["maxValue"].(float64)
			if v, ok := val["value"].(float64); ok && lo == 0 && hi == 0 {
				lo, hi = v, v
			}
			if lo > 0 || hi > 0 {
				j.SalaryMin, j.SalaryMax, j.Currency = int(lo), int(hi), ldString(sal["currency"])
			}
		}
	}
	return j
}
