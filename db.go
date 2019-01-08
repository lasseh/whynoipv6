package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Site is the database structure
type Site struct {
	Rank          int       `json:"rank"`
	Hostname      string    `json:"hostname"`
	IPv6          bool      `json:"ipv6"`
	NSIPv6        bool      `json:"nsipv6"`
	IPv6CreatedAt time.Time `json:"-"`
	Checked       bool      `json:"-"`
	Nsv6checked   bool      `json:"-"`
	Country       string    `json:"country"`
}

// Stats lists statistics for the sites
type Stats struct {
	Sites         int     `json:"sites"`
	SitesWithV6   int     `json:"sites_with_v6"`
	SitesWithNSV6 int     `json:"sites_with_nsv6"`
	Top1kV6       int     `json:"top_1k_v6"`
	Top1kNSV6     int     `json:"top_1k_nsv6"`
	V6Percent     float64 `json:"v6_percent"`
}

// CountryStat gets data from the countries table
type CountryStat struct {
	CountryName string
	CountryCode string
	Sites       int
	V6sites     int
	Percent     float64
}

// Country is a map of country codes to country names
type Country struct {
	CountryName string
	CountryCode string
}

// Asn is a aggregate of sites per as number and how many sites belong to them
type Asn struct {
	Asn       int
	Asname    string
	CountV4   int
	CountV6   int
	PercentV4 float64
	PercentV6 float64
}

// getStats returns stats for all sites
// TODO: make a view out of this
func getStats() Stats {
	var s Stats

	db.Table("sites").Count(&s.Sites)
	db.Table("sites").Where("checked = true AND ipv6 = true").Count(&s.SitesWithV6)
	db.Table("sites").Where("checked = true AND ns_ipv6 = true").Count(&s.SitesWithNSV6)
	db.Table("sites").Where("checked = true AND ipv6 = true AND rank < 1000").Count(&s.Top1kV6)
	db.Table("sites").Where("checked = true AND ns_ipv6 = true AND rank < 1000").Count(&s.Top1kNSV6)

	// Calculate the percentage of sites with IPv6
	var v6 float64
	v6 = PercentOf(s.SitesWithV6, s.Sites)
	s.V6Percent = math.Round(v6*10) / 10

	return s
}

// getSites returns a list of the top 100 alexa sites missing IPv6
func getSites(offset, limit int) []Site {
	var s []Site

	// Set default values
	if offset == 0 {
		offset = 0
	}
	if limit == 0 {
		limit = 100
	}

	db.Where("ipv6 IS false AND checked = true").Offset(offset).Limit(limit).Order("rank").Find(&s)

	return s
}

// getCountry returns a list of the top 50 sites without IPv6 for a given country code
func getCountry(countryCode string) []Site {
	var s []Site

	countryCode = strings.ToUpper(countryCode)

	if err := db.Where("ipv6 IS false AND country = ?", countryCode).Limit(50).Order("rank").Find(&s).Error; err != nil {
		return nil
	}
	return s
}

// getCountryList returns a list of the top 50 cointries without IPv6
func getCountryList() []CountryStat {
	var s []CountryStat
	if err := db.Table("countries").Where("sites > 1 and v6sites > 1").Limit(50).Order("sites").Find(&s).Error; err != nil {
		return nil
	}
	return s
}

// getCountryV6 returns a list of the top 50 sites with IPv6 for a given country code
func getCountryV6(countryCode string) []Site {
	var s []Site

	countryCode = strings.ToUpper(countryCode)

	if err := db.Where("ipv6 IS true AND country = ?", countryCode).Limit(50).Order("rank").Find(&s).Error; err != nil {
		return nil
	}
	return s
}

// getCountryName returns the full name from a country code
func getCountryName(countryCode string) Country {
	var c Country
	countryCode = strings.ToUpper(countryCode)

	if err := db.Table("countries").Where("country_code = ?", countryCode).First(&c).Error; err != nil {
		fmt.Println(err)
	}
	return c
}

// getCountryStats returns ...
// replace with stats from countries table?
func getCountryStats(countryCode string) Stats {
	var s Stats
	countryCode = strings.ToUpper(countryCode)

	db.Table("sites").Where("checked = true AND country = ?", countryCode).Count(&s.Sites)
	db.Table("sites").Where("checked = true AND ipv6 = true AND country = ?", countryCode).Count(&s.SitesWithV6)
	db.Table("sites").Where("checked = true AND ns_ipv6 = true AND country = ?", countryCode).Count(&s.SitesWithNSV6)

	// Calculate the percentage of sites with IPv6
	var v6 float64
	v6 = PercentOf(s.SitesWithV6, s.Sites)
	s.V6Percent = math.Round(v6*10) / 10

	return s
}

func getCountryStatList() []CountryStat {
	var s []CountryStat
	db.Table("countries").Where("v6sites IS NOT NULL").Order("v6sites desc").Limit("50").Find(&s)
	return s
}

func getASNList(order string) []Asn {
	var s []Asn
	if order == "ipv6" {
		db.Table("asn").Where("count_v4 > 1000 and count_v6 > 1").Order("count_v6 desc").Limit("40").Find(&s)
	} else if order == "percent" {
		db.Table("asn").Where("count_v4 > 1000 and count_v6 > 1").Order("percent_v6 desc").Limit("40").Find(&s)
	} else { // ipv4 order and catch all
		db.Table("asn").Where("count_v4 > 1000 and count_v6 > 1").Order("count_v4 desc").Limit("40").Find(&s)
	}
	return s
}

func getASN(asn int) Asn {
	var s Asn
	db.Table("asn").Where("asn = ?", asn).Find(&s)
	return s
}

// PercentOf calculate [number1] is what percent of [number2]
func PercentOf(current int, all int) float64 {
	percent := (float64(current) * float64(100)) / float64(all)
	return percent
}
