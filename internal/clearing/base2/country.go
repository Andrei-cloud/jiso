package base2

import (
	"strings"
)

// CountryInfo represents ISO 3166-1 country codes and name.
type CountryInfo struct {
	Alpha2  string
	Alpha3  string
	Numeric string
	Name    string
}

// MerchantLocationDetails holds parsed components of ISO 8583 Field 43.
type MerchantLocationDetails struct {
	MerchantName    string
	MerchantCity    string
	CountryNumeric  string // 3-digit numeric for Base II TCR 0 (e.g. "840", "784", "124")
	CountryAlpha2   string // 2-letter alpha (e.g. "US", "CA", "AE")
	CountryAlpha3   string // 3-letter alpha (e.g. "USA", "CAN", "ARE")
	StateProvince   string // 3-character formatted state code for Base II TCR 0 (e.g. "CA ", "ON ", "   ")
}

var usStates = map[string]string{
	"AL": "Alabama", "AK": "Alaska", "AZ": "Arizona", "AR": "Arkansas",
	"CA": "California", "CO": "Colorado", "CT": "Connecticut", "DE": "Delaware",
	"FL": "Florida", "GA": "Georgia", "HI": "Hawaii", "ID": "Idaho",
	"IL": "Illinois", "IN": "Indiana", "IA": "Iowa", "KS": "Kansas",
	"KY": "Kentucky", "LA": "Louisiana", "ME": "Maine", "MD": "Maryland",
	"MA": "Massachusetts", "MI": "Michigan", "MN": "Minnesota", "MS": "Mississippi",
	"MO": "Missouri", "MT": "Montana", "NE": "Nebraska", "NV": "Nevada",
	"NH": "New Hampshire", "NJ": "New Jersey", "NM": "New Mexico", "NY": "New York",
	"NC": "North Carolina", "ND": "North Dakota", "OH": "Ohio", "OK": "Oklahoma",
	"OR": "Oregon", "PA": "Pennsylvania", "RI": "Rhode Island", "SC": "South Carolina",
	"SD": "South Dakota", "TN": "Tennessee", "TX": "Texas", "UT": "Utah",
	"VT": "Vermont", "VA": "Virginia", "WA": "Washington", "WV": "West Virginia",
	"WI": "Wisconsin", "WY": "Wyoming", "DC": "District of Columbia",
	// US Territories and Armed Forces
	"PR": "Puerto Rico", "VI": "Virgin Islands", "GU": "Guam",
	"AS": "American Samoa", "MP": "Northern Mariana Islands",
	"AE": "Armed Forces Europe", "AP": "Armed Forces Pacific", "AA": "Armed Forces Americas",
}

var caProvinces = map[string]string{
	"AB": "Alberta", "BC": "British Columbia", "MB": "Manitoba",
	"NB": "New Brunswick", "NL": "Newfoundland and Labrador",
	"NS": "Nova Scotia", "NT": "Northwest Territories", "NU": "Nunavut",
	"ON": "Ontario", "PE": "Prince Edward Island", "QC": "Quebec",
	"SK": "Saskatchewan", "YT": "Yukon",
}

var countryList = []CountryInfo{
	{"AF", "AFG", "004", "Afghanistan"},
	{"AL", "ALB", "008", "Albania"},
	{"DZ", "DZA", "012", "Algeria"},
	{"AD", "AND", "020", "Andorra"},
	{"AO", "AGO", "024", "Angola"},
	{"AG", "ATG", "028", "Antigua and Barbuda"},
	{"AR", "ARG", "032", "Argentina"},
	{"AM", "ARM", "051", "Armenia"},
	{"AU", "AUS", "036", "Australia"},
	{"AT", "AUT", "040", "Austria"},
	{"AZ", "AZE", "031", "Azerbaijan"},
	{"BS", "BHS", "044", "Bahamas"},
	{"BH", "BHR", "048", "Bahrain"},
	{"BD", "BGD", "050", "Bangladesh"},
	{"BB", "BRB", "052", "Barbados"},
	{"BY", "BLR", "112", "Belarus"},
	{"BE", "BEL", "056", "Belgium"},
	{"BZ", "BLZ", "084", "Belize"},
	{"BJ", "BEN", "204", "Benin"},
	{"BT", "BTN", "064", "Bhutan"},
	{"BO", "BOL", "068", "Bolivia"},
	{"BA", "BIH", "070", "Bosnia and Herzegovina"},
	{"BW", "BWA", "072", "Botswana"},
	{"BR", "BRA", "076", "Brazil"},
	{"BN", "BRN", "096", "Brunei Darussalam"},
	{"BG", "BGR", "100", "Bulgaria"},
	{"BF", "BFA", "854", "Burkina Faso"},
	{"BI", "BDI", "108", "Burundi"},
	{"KH", "KHM", "116", "Cambodia"},
	{"CM", "CMR", "120", "Cameroon"},
	{"CA", "CAN", "124", "Canada"},
	{"CV", "CPV", "132", "Cape Verde"},
	{"KY", "CYM", "136", "Cayman Islands"},
	{"CF", "CAF", "140", "Central African Republic"},
	{"TD", "TCD", "148", "Chad"},
	{"CL", "CHL", "152", "Chile"},
	{"CN", "CHN", "156", "China"},
	{"CO", "COL", "170", "Colombia"},
	{"KM", "COM", "174", "Comoros"},
	{"CG", "COG", "178", "Congo"},
	{"CD", "COD", "180", "Congo, Democratic Republic"},
	{"CR", "CRI", "188", "Costa Rica"},
	{"CI", "CIV", "384", "Cote d'Ivoire"},
	{"HR", "HRV", "191", "Croatia"},
	{"CU", "CUB", "192", "Cuba"},
	{"CY", "CYP", "196", "Cyprus"},
	{"CZ", "CZE", "203", "Czech Republic"},
	{"DK", "DNK", "208", "Denmark"},
	{"DJ", "DJI", "262", "Djibouti"},
	{"DM", "DMA", "212", "Dominica"},
	{"DO", "DOM", "214", "Dominican Republic"},
	{"EC", "ECU", "218", "Ecuador"},
	{"EG", "EGY", "818", "Egypt"},
	{"SV", "SLV", "222", "El Salvador"},
	{"EE", "EST", "233", "Estonia"},
	{"ET", "ETH", "231", "Ethiopia"},
	{"FJ", "FJI", "242", "Fiji"},
	{"FI", "FIN", "246", "Finland"},
	{"FR", "FRA", "250", "France"},
	{"GA", "GAB", "266", "Gabon"},
	{"GM", "GMB", "270", "Gambia"},
	{"GE", "GEO", "268", "Georgia"},
	{"DE", "DEU", "276", "Germany"},
	{"GH", "GHA", "288", "Ghana"},
	{"GR", "GRC", "300", "Greece"},
	{"GT", "GTM", "320", "Guatemala"},
	{"GN", "GIN", "324", "Guinea"},
	{"GY", "GUY", "328", "Guyana"},
	{"HT", "HTI", "332", "Haiti"},
	{"HN", "HND", "340", "Honduras"},
	{"HK", "HKG", "344", "Hong Kong"},
	{"HU", "HUN", "348", "Hungary"},
	{"IS", "ISL", "352", "Iceland"},
	{"IN", "IND", "356", "India"},
	{"ID", "IDN", "360", "Indonesia"},
	{"IR", "IRN", "364", "Iran"},
	{"IQ", "IRQ", "368", "Iraq"},
	{"IE", "IRL", "372", "Ireland"},
	{"IL", "ISR", "376", "Israel"},
	{"IT", "ITA", "380", "Italy"},
	{"JM", "JAM", "388", "Jamaica"},
	{"JP", "JPN", "392", "Japan"},
	{"JO", "JOR", "400", "Jordan"},
	{"KZ", "KAZ", "398", "Kazakhstan"},
	{"KE", "KEN", "404", "Kenya"},
	{"KW", "KWT", "414", "Kuwait"},
	{"KG", "KGZ", "417", "Kyrgyzstan"},
	{"LV", "LVA", "428", "Latvia"},
	{"LB", "LBN", "422", "Lebanon"},
	{"LY", "LBY", "434", "Libya"},
	{"LI", "LIE", "438", "Liechtenstein"},
	{"LT", "LTU", "440", "Lithuania"},
	{"LU", "LUX", "442", "Luxembourg"},
	{"MO", "MAC", "446", "Macao"},
	{"MY", "MYS", "458", "Malaysia"},
	{"MV", "MDV", "462", "Maldives"},
	{"MT", "MLT", "470", "Malta"},
	{"MX", "MEX", "484", "Mexico"},
	{"MC", "MCO", "492", "Monaco"},
	{"MN", "MNG", "496", "Mongolia"},
	{"ME", "MNE", "499", "Montenegro"},
	{"MA", "MAR", "504", "Morocco"},
	{"MZ", "MOZ", "508", "Mozambique"},
	{"NL", "NLD", "528", "Netherlands"},
	{"NZ", "NZL", "554", "New Zealand"},
	{"NG", "NGA", "566", "Nigeria"},
	{"NO", "NOR", "578", "Norway"},
	{"OM", "OMN", "512", "Oman"},
	{"PK", "PAK", "586", "Pakistan"},
	{"PA", "PAN", "591", "Panama"},
	{"PY", "PRY", "600", "Paraguay"},
	{"PE", "PER", "604", "Peru"},
	{"PH", "PHL", "608", "Philippines"},
	{"PL", "POL", "616", "Poland"},
	{"PT", "PRT", "620", "Portugal"},
	{"QA", "QAT", "634", "Qatar"},
	{"RO", "ROU", "642", "Romania"},
	{"RU", "RUS", "643", "Russian Federation"},
	{"SA", "SAU", "682", "Saudi Arabia"},
	{"RS", "SRB", "688", "Serbia"},
	{"SG", "SGP", "702", "Singapore"},
	{"SK", "SVK", "703", "Slovakia"},
	{"SI", "SVN", "705", "Slovenia"},
	{"ZA", "ZAF", "710", "South Africa"},
	{"KR", "KOR", "410", "South Korea"},
	{"ES", "ESP", "724", "Spain"},
	{"LK", "LKA", "144", "Sri Lanka"},
	{"SE", "SWE", "752", "Sweden"},
	{"CH", "CHE", "756", "Switzerland"},
	{"TW", "TWN", "158", "Taiwan"},
	{"TH", "THA", "764", "Thailand"},
	{"TN", "TUN", "788", "Tunisia"},
	{"TR", "TUR", "792", "Turkey"},
	{"UA", "UKR", "804", "Ukraine"},
	{"AE", "ARE", "784", "United Arab Emirates"},
	{"GB", "GBR", "826", "United Kingdom"},
	{"US", "USA", "840", "United States"},
	{"UY", "URY", "858", "Uruguay"},
	{"UZ", "UZB", "860", "Uzbekistan"},
	{"VE", "VEN", "862", "Venezuela"},
	{"VN", "VNM", "704", "Viet Nam"},
}

var (
	byAlpha2  = make(map[string]CountryInfo)
	byAlpha3  = make(map[string]CountryInfo)
	byNumeric = make(map[string]CountryInfo)
)

func init() {
	for _, c := range countryList {
		byAlpha2[strings.ToUpper(c.Alpha2)] = c
		byAlpha3[strings.ToUpper(c.Alpha3)] = c
		byNumeric[c.Numeric] = c
	}
}

// LookupCountryByAlpha2 finds country by 2-letter ISO code.
func LookupCountryByAlpha2(alpha2 string) (CountryInfo, bool) {
	c, ok := byAlpha2[strings.ToUpper(strings.TrimSpace(alpha2))]
	return c, ok
}

// LookupCountryByAlpha3 finds country by 3-letter ISO code.
func LookupCountryByAlpha3(alpha3 string) (CountryInfo, bool) {
	c, ok := byAlpha3[strings.ToUpper(strings.TrimSpace(alpha3))]
	return c, ok
}

// LookupCountryByNumeric finds country by 3-digit numeric code.
func LookupCountryByNumeric(numeric string) (CountryInfo, bool) {
	c, ok := byNumeric[strings.TrimSpace(numeric)]
	return c, ok
}

// ParseMerchantLocation parses ISO 8583 Field 43 (40 characters) into merchant name, city, country, and state.
//
// Rules:
// - Chars 1-25: Merchant Name (25 chars)
// - Chars 26-37 or 26-38: Merchant City (12 or 13 chars)
// - Chars 38-40 (3 chars): 3-character ISO Alpha-3 Country code (e.g. "ARE", "GBR", "FRA", "DEU", "JPN", "USA", "CAN").
//   - Country is looked up, State is "   ".
// - Chars 39-40 (2 chars):
//   - For US: 2-character State code (e.g. "CA", "NY", "TX", "AE", etc.). Country is "840" (US), State is "CA ".
//   - For Canada: 2-character Province code (e.g. "ON", "QC", "BC"). Country is "124" (CA), State is "ON ".
//   - For International: 2-character ISO Alpha-2 Country code (e.g. "AE", "GB", "FR"). Country is looked up, State is "   ".
func ParseMerchantLocation(loc string, currencyCode string) MerchantLocationDetails {
	res := MerchantLocationDetails{
		MerchantName:   "MER NAME TEST",
		MerchantCity:   "MCITY TEST",
		CountryNumeric: "840",
		CountryAlpha2:  "US",
		CountryAlpha3:  "USA",
		StateProvince:  "   ",
	}

	trimmed := strings.TrimRight(loc, " ")
	if len(trimmed) == 0 {
		if c, ok := LookupCountryByNumeric(currencyCode); ok {
			res.CountryNumeric = c.Numeric
			res.CountryAlpha2 = c.Alpha2
			res.CountryAlpha3 = c.Alpha3
		}
		return res
	}

	// 1. Extract Merchant Name (up to 25 chars)
	if len(loc) >= 25 {
		res.MerchantName = strings.TrimSpace(loc[:25])
	} else {
		res.MerchantName = strings.TrimSpace(loc)
		return res
	}

	if len(loc) < 38 {
		if len(loc) > 25 {
			res.MerchantCity = strings.TrimSpace(loc[25:])
		}
		return res
	}

	// Check 3-character country code at positions 37..40 (chars 38, 39, 40)
	if len(loc) >= 40 {
		suffix3 := strings.ToUpper(strings.TrimSpace(loc[37:40]))
		if len(suffix3) == 3 {
			if country, ok := LookupCountryByAlpha3(suffix3); ok {
				res.MerchantCity = strings.TrimSpace(loc[25:37])
				res.CountryNumeric = country.Numeric
				res.CountryAlpha2 = country.Alpha2
				res.CountryAlpha3 = country.Alpha3
				res.StateProvince = "   "
				return res
			}
			if country, ok := LookupCountryByNumeric(suffix3); ok {
				res.MerchantCity = strings.TrimSpace(loc[25:37])
				res.CountryNumeric = country.Numeric
				res.CountryAlpha2 = country.Alpha2
				res.CountryAlpha3 = country.Alpha3
				res.StateProvince = "   "
				return res
			}
		}
	}

	// Check 2-character state or country code at positions 38..40 (chars 39, 40)
	res.MerchantCity = strings.TrimSpace(loc[25:38])
	suffix2 := ""
	if len(loc) >= 40 {
		suffix2 = strings.ToUpper(strings.TrimSpace(loc[38:40]))
	} else if len(loc) >= 38 {
		suffix2 = strings.ToUpper(strings.TrimSpace(loc[38:]))
	}

	if len(suffix2) == 2 {
		// US State
		if _, isUSState := usStates[suffix2]; isUSState {
			res.CountryNumeric = "840"
			res.CountryAlpha2 = "US"
			res.CountryAlpha3 = "USA"
			res.StateProvince = suffix2 + " "
			return res
		}

		// Canadian Province
		if _, isCAProv := caProvinces[suffix2]; isCAProv {
			res.CountryNumeric = "124"
			res.CountryAlpha2 = "CA"
			res.CountryAlpha3 = "CAN"
			res.StateProvince = suffix2 + " "
			return res
		}

		// Alpha2 Country
		if country, ok := LookupCountryByAlpha2(suffix2); ok {
			res.CountryNumeric = country.Numeric
			res.CountryAlpha2 = country.Alpha2
			res.CountryAlpha3 = country.Alpha3
			res.StateProvince = "   "
			return res
		}
	}

	// Fallback to currency code
	if c, ok := LookupCountryByNumeric(currencyCode); ok {
		res.CountryNumeric = c.Numeric
		res.CountryAlpha2 = c.Alpha2
		res.CountryAlpha3 = c.Alpha3
	}

	return res
}

