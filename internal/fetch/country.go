package fetch

import "strings"

// countryNames maps ISO 3166-1 alpha-2 codes, as some boards and schema.org
// data publish them, to the names location filters are written in.
var countryNames = map[string]string{
	"AD": "Andorra", "AL": "Albania", "AT": "Austria", "BA": "Bosnia and Herzegovina",
	"BE": "Belgium", "BG": "Bulgaria", "BY": "Belarus", "CH": "Switzerland", "CY": "Cyprus",
	"CZ": "Czechia", "DE": "Germany", "DK": "Denmark", "EE": "Estonia", "ES": "Spain",
	"FI": "Finland", "FR": "France", "GB": "United Kingdom", "GR": "Greece", "HR": "Croatia",
	"HU": "Hungary", "IE": "Ireland", "IS": "Iceland", "IT": "Italy", "LI": "Liechtenstein",
	"LT": "Lithuania", "LU": "Luxembourg", "LV": "Latvia", "MC": "Monaco", "MD": "Moldova",
	"ME": "Montenegro", "MK": "North Macedonia", "MT": "Malta", "NL": "Netherlands",
	"NO": "Norway", "PL": "Poland", "PT": "Portugal", "RO": "Romania", "RS": "Serbia",
	"SE": "Sweden", "SI": "Slovenia", "SK": "Slovakia", "UA": "Ukraine", "UK": "United Kingdom",
	"TR": "Turkey", "IL": "Israel", "AE": "United Arab Emirates", "GE": "Georgia", "AM": "Armenia",
	"US": "United States", "CA": "Canada", "MX": "Mexico", "BR": "Brazil", "AR": "Argentina",
	"CL": "Chile", "CO": "Colombia", "IN": "India", "SG": "Singapore", "JP": "Japan",
	"KR": "South Korea", "CN": "China", "AU": "Australia", "NZ": "New Zealand", "ZA": "South Africa",
}

// countryName expands a two-letter country code; anything else is returned
// unchanged.
func countryName(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 2 {
		if name, ok := countryNames[strings.ToUpper(s)]; ok {
			return name
		}
	}
	return s
}
