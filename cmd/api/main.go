package main

import (
	"fmt"
	"log"
	"net/http"
	"whynoipv6/internal/config"
	"whynoipv6/internal/core"
	"whynoipv6/internal/postgres"
	"whynoipv6/internal/rest"
)

func main() {
	log.Println("Starting api server")

	// Read config
	cfg, err := config.Read()
	if err != nil {
		log.Fatal(err.Error())
	}

	// Connect to the database
	db, err := postgres.NewPostgreSQL(cfg.DatabaseSource)
	if err != nil {
		log.Println("Error connecting to database", err)
	}

	// Create Router
	router, err := rest.NewRouter()
	if err != nil {
		fmt.Println(err)
	}

	// Create Core Services
	changelogService := core.NewChangelogService(db)
	domainService := core.NewDomainService(db)
	countryService := core.NewCountryService(db)
	campaignService := core.NewCampaignService(db)
	metricService := core.NewMetricService(db)

	// Mount the API onto the router
	router.Mount("/api/domain", rest.DomainHandler{Repo: domainService}.Routes())
	router.Mount("/api/country", rest.CountryHandler{Repo: countryService}.Routes())
	router.Mount("/api/changelog", rest.ChangelogHandler{Repo: changelogService}.Routes())
	router.Mount("/api/campaign", rest.CampaignHandler{Repo: campaignService}.Routes())
	router.Mount("/api/metric", rest.MetricHandler{Repo: metricService}.Routes())

	rest.PrintRoutes(router)

	// Start the server
	log.Printf("Starting Server on port %s", cfg.APIPort)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("[::1]:%v", cfg.APIPort), router))

}
