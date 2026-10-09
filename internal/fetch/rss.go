package fetch

import (
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// rssFeed is an RSS 2.0 document whose items decode into T.
type rssFeed[T any] struct {
	XMLName xml.Name
	Channel struct {
		Title string `xml:"title"`
		Items []T    `xml:"item"`
	} `xml:"channel"`
}

// decodeRSS decodes an RSS 2.0 feed. An HTML page (a moved or blocked feed)
// often decodes without error, so the root element is checked too; otherwise
// such a source would quietly report zero jobs.
func decodeRSS[T any](r io.Reader) (*rssFeed[T], error) {
	var feed rssFeed[T]
	if err := xml.NewDecoder(r).Decode(&feed); err != nil {
		return nil, fmt.Errorf("not an RSS feed: %w", err)
	}
	if feed.XMLName.Local != "rss" {
		return nil, fmt.Errorf("not an RSS feed: root element is <%s>", feed.XMLName.Local)
	}
	return &feed, nil
}

// rssTime parses an RSS pubDate, returning the zero time if it is malformed.
func rssTime(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC1123Z, time.RFC1123} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

var (
	salaryThousandsSep = regexp.MustCompile(`(\d)[,.](\d{3})\b`)
	salaryNumber       = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(k)?\b`)
)

// parseSalary reads an annual range from free text such as "$90k - $105k"
// or "€80,000–100,000". Hourly, daily or monthly rates and anything it can't
// read confidently yield ok=false.
func parseSalary(s string) (minSal, maxSal int, currency string, ok bool) {
	l := strings.ToLower(s)
	for _, period := range []string{"hour", "/h", "/hr", "day", "month", "/mo", "week"} {
		if strings.Contains(l, period) {
			return 0, 0, "", false
		}
	}
	switch {
	case strings.Contains(s, "$"):
		currency = "USD"
	case strings.Contains(s, "€"):
		currency = "EUR"
	case strings.Contains(s, "£"):
		currency = "GBP"
	}

	l = salaryThousandsSep.ReplaceAllString(salaryThousandsSep.ReplaceAllString(l, "$1$2"), "$1$2")
	matches := salaryNumber.FindAllStringSubmatch(l, 2)
	// "90-105k": a "k" on the second number applies to the first as well.
	rangeK := len(matches) == 2 && matches[1][2] == "k"
	var vals []int
	for _, m := range matches {
		f, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, 0, "", false
		}
		if m[2] == "k" || (rangeK && f < 1000) {
			f *= 1000
		}
		if f < 10000 { // "90 - 150" without a unit is ambiguous
			return 0, 0, "", false
		}
		vals = append(vals, int(f))
	}
	switch len(vals) {
	case 1:
		return vals[0], vals[0], currency, true
	case 2:
		return min(vals[0], vals[1]), max(vals[0], vals[1]), currency, true
	}
	return 0, 0, "", false
}
