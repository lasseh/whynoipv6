package main

// This cli tool generates stats for how many sites has v6 enabled
// This should run every night

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"time"

	"github.com/gobuffalo/envy"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

var db *gorm.DB
var err error

// Site is a bad name
type Site struct {
	ID            int
	Rank          int
	Hostname      string
	IPv6          bool
	NSIPv6        bool
	IPv6CreatedAt time.Time
	Checked       bool
	Nsv6checked   bool
	Country       string
}

// Stats lists statistics for the sites
type Stats struct {
	Sites   int
	Ipv6    int
	Ns      int
	Topv6   int
	Topns   int
	Percent float64
}

// Asn is also a bad name
type Asn struct {
	Asn       int
	Asname    string
	CountV4   int
	CountV6   int
	PercentV4 float64
	PercentV6 float64
}

type bgpInfo struct {
	Status        string `json:"status"`
	StatusMessage string `json:"status_message"`
	Data          struct {
		Asn               int         `json:"asn"`
		Name              string      `json:"name"`
		DescriptionShort  string      `json:"description_short"`
		DescriptionFull   []string    `json:"description_full"`
		CountryCode       string      `json:"country_code"`
		Website           interface{} `json:"website"`
		EmailContacts     []string    `json:"email_contacts"`
		AbuseContacts     []string    `json:"abuse_contacts"`
		LookingGlass      interface{} `json:"looking_glass"`
		TrafficEstimation interface{} `json:"traffic_estimation"`
		TrafficRatio      interface{} `json:"traffic_ratio"`
		OwnerAddress      []string    `json:"owner_address"`
		RirAllocation     struct {
			RirName          string `json:"rir_name"`
			CountryCode      string `json:"country_code"`
			DateAllocated    string `json:"date_allocated"`
			AllocationStatus string `json:"allocation_status"`
		} `json:"rir_allocation"`
		IanaAssignment struct {
			AssignmentStatus string      `json:"assignment_status"`
			Description      string      `json:"description"`
			WhoisServer      string      `json:"whois_server"`
			DateAssigned     interface{} `json:"date_assigned"`
		} `json:"iana_assignment"`
		DateUpdated string `json:"date_updated"`
	} `json:"data"`
	Meta struct {
		TimeZone      string `json:"time_zone"`
		APIVersion    int    `json:"api_version"`
		ExecutionTime string `json:"execution_time"`
	} `json:"@meta"`
}

func main() {
	// Load .env file
	envy.Load("../../.env", "$GOROOT/src/github.com/lasseh/whynoipv6/.env")

	// Database connection
	dsn := fmt.Sprintf("user=%s password=%s dbname=%s host=%s port=%s sslmode=disable",
		envy.Get("V6_USER", ""),
		envy.Get("V6_PASS", ""),
		envy.Get("V6_DB", ""),
		envy.Get("V6_HOST", "localhost"),
		envy.Get("V6_PORT", "5432"),
	)
	db, err = gorm.Open("postgres", dsn)
	if err != nil {
		fmt.Println("Error connecting to database:", err)
	}
	defer db.Close()
	// db.LogMode(true)

	// v6 stats for all sites
	// this will be used to generate graphs for the progression of v6 deployment
	var s Stats

	db.Table("sites").Where("checked = true").Count(&s.Sites)
	db.Table("sites").Where("checked = true AND ipv6 = true").Count(&s.Ipv6)
	db.Table("sites").Where("checked = true AND ns_ipv6 = true").Count(&s.Ns)
	db.Table("sites").Where("checked = true AND ipv6 = true AND rank < 1000").Count(&s.Topv6)
	db.Table("sites").Where("checked = true AND ns_ipv6 = true AND rank < 1000").Count(&s.Topns)

	// Calculate the percentage of sites with IPv6
	var v6 float64
	v6 = PercentOf(s.Ipv6, s.Sites)
	s.Percent = math.Round(v6*10) / 10

	// Push to database
	// This creates stats for the total of all sites
	// run this every day so we can generate graphs from the data
	db.Table("stats").Create(&s)

	//
	// Generate stats per ASN
	//
	var a []Asn
	db.Table("sites").Select("asn").Where("asn > 1").Group("asn").Find(&a)
	for _, v := range a {
		// Count v4 for current asn
		db.Table("sites").Select("count(asn) as count_v4").Where("ipv6 = FALSE AND asn = ?", v.Asn).Group("asn").First(&v)

		// Count v6 for current asn
		db.Table("sites").Select("count(asn) as count_v6").Where("ipv6 = TRUE AND asn = ?", v.Asn).Group("asn").First(&v)

		// Get current asname
		db.Table("asn").Select("asname").Where("asn = ?", v.Asn).First(&v)

		sum := v.CountV4 + v.CountV6

		// Calculate v4 percent for current asn
		v.PercentV4 = PercentOf(v.CountV4, sum)

		// Calculate v6 percent for current asn
		v.PercentV6 = PercentOf(v.CountV6, sum)

		// Get ASN Name
		if len(v.Asname) == 0 {
			v.Asname, err = asName(v.Asn)
			if err != nil {
				fmt.Println("Error getting ASN, skipping for now")
				v.Asname = ""
			}
		}

		// Update
		r := db.Exec("UPDATE asn SET count_v4 = ?, count_v6 = ?, percent_v4 = ?, percent_v6 = ?, asname = ? WHERE asn = ?", v.CountV4, v.CountV6, v.PercentV4, v.PercentV6, v.Asname, v.Asn)
		// Insert row if not exists
		if r.RowsAffected == 0 {
			db.Table("asn").Save(&v)
		}

		// Print
		fmt.Printf("ASN: %v CountV4: %v CountV6: %v\n", v.Asn, v.CountV4, v.CountV6)
	}
}

// PercentOf calculate [number1] is what percent of [number2]
func PercentOf(current int, all int) float64 {
	percent := (float64(current) * float64(100)) / float64(all)
	return percent
}

func asName(asn int) (string, error) {
	// HTTP Client
	var netTransport = &http.Transport{
		Dial: (&net.Dialer{
			Timeout: 5 * time.Second,
		}).Dial,
		TLSHandshakeTimeout: 5 * time.Second,
	}
	client := &http.Client{
		Timeout:   time.Second * 10,
		Transport: netTransport,
	}
	req, _ := http.NewRequest("GET", fmt.Sprintf("https://api.bgpview.io/asn/%d", asn), nil)
	resp, err := client.Do(req)

	if err != nil {
		fmt.Println("Errored when sending request to the server")
		return "", err
	}

	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)

	var info bgpInfo
	err = json.Unmarshal(body, &info)
	if err != nil {
		fmt.Println("error:", err)
	}
	fmt.Println("AS Name:", info.Data.DescriptionShort)

	// Sleep so we dont get banned from bgpview.io
	time.Sleep(1 * time.Second)

	return info.Data.DescriptionShort, nil
}
