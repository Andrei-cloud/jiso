package base2

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLookupCountry(t *testing.T) {
	// Alpha 2
	c, ok := LookupCountryByAlpha2("US")
	assert.True(t, ok)
	assert.Equal(t, "840", c.Numeric)
	assert.Equal(t, "USA", c.Alpha3)

	c, ok = LookupCountryByAlpha2("AE")
	assert.True(t, ok)
	assert.Equal(t, "784", c.Numeric)
	assert.Equal(t, "ARE", c.Alpha3)

	// Alpha 3
	c, ok = LookupCountryByAlpha3("GBR")
	assert.True(t, ok)
	assert.Equal(t, "826", c.Numeric)
	assert.Equal(t, "GB", c.Alpha2)

	// Numeric
	c, ok = LookupCountryByNumeric("250")
	assert.True(t, ok)
	assert.Equal(t, "FRA", c.Alpha3)
	assert.Equal(t, "FR", c.Alpha2)
}

func TestParseMerchantLocation_USState(t *testing.T) {
	loc := "STARBUCKS #1234          SEATTLE      WA"
	details := ParseMerchantLocation(loc, "840")

	assert.Equal(t, "STARBUCKS #1234", details.MerchantName)
	assert.Equal(t, "SEATTLE", details.MerchantCity)
	assert.Equal(t, "840", details.CountryNumeric)
	assert.Equal(t, "US", details.CountryAlpha2)
	assert.Equal(t, "WA ", details.StateProvince)
}

func TestParseMerchantLocation_CanadianProvince(t *testing.T) {
	loc := "TIM HORTONS #5678        TORONTO      ON"
	details := ParseMerchantLocation(loc, "124")

	assert.Equal(t, "TIM HORTONS #5678", details.MerchantName)
	assert.Equal(t, "TORONTO", details.MerchantCity)
	assert.Equal(t, "124", details.CountryNumeric)
	assert.Equal(t, "CA", details.CountryAlpha2)
	assert.Equal(t, "ON ", details.StateProvince)
}

func TestParseMerchantLocation_InternationalAlpha3(t *testing.T) {
	// UAE
	loc := "DUBAI MALL FASHION       DUBAI       ARE"
	details := ParseMerchantLocation(loc, "784")

	assert.Equal(t, "DUBAI MALL FASHION", details.MerchantName)
	assert.Equal(t, "DUBAI", details.MerchantCity)
	assert.Equal(t, "784", details.CountryNumeric)
	assert.Equal(t, "AE", details.CountryAlpha2)
	assert.Equal(t, "   ", details.StateProvince)

	// UK
	locUK := "HARRODS DEPARTMENT STORE LONDON      GBR"
	detailsUK := ParseMerchantLocation(locUK, "826")
	assert.Equal(t, "826", detailsUK.CountryNumeric)
	assert.Equal(t, "GB", detailsUK.CountryAlpha2)
	assert.Equal(t, "   ", detailsUK.StateProvince)

	// France
	locFR := "PARIS LUXURY STORE       PARIS       FRA"
	detailsFR := ParseMerchantLocation(locFR, "978")
	assert.Equal(t, "250", detailsFR.CountryNumeric)
	assert.Equal(t, "FR", detailsFR.CountryAlpha2)
	assert.Equal(t, "   ", detailsFR.StateProvince)
}

func TestParseMerchantLocation_ShortOrEmpty(t *testing.T) {
	details := ParseMerchantLocation("", "840")
	assert.Equal(t, "840", details.CountryNumeric)
	assert.Equal(t, "US", details.CountryAlpha2)

	detailsShort := ParseMerchantLocation("LOCAL SHOP", "840")
	assert.Equal(t, "LOCAL SHOP", detailsShort.MerchantName)
}
