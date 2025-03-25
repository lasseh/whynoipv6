// Package main contains the entry point for the whynoipv6 API server.
package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"whynoipv6/internal/config"
	"whynoipv6/internal/core"
	"whynoipv6/internal/postgres"
	"whynoipv6/internal/rest"
)

func main() {
	log.Println("Starting api server")

	// Read the application configuration.
	cfg, err := config.Read()
	if err != nil {
		log.Fatalf("Failed to read config: %v", err)
	}

	// Connect to the database
	const maxRetries = 5
	const timeout = 10 * time.Second
	dbSource := cfg.DatabaseSource + "&application_name=api"
	db, err := postgres.NewPostgreSQL(dbSource, maxRetries, timeout)
	if err != nil {
		log.Fatalln("Error connecting to database", err)
	}
	defer db.Close()

	// Initialize the router for handling HTTP requests.
	router, err := rest.NewRouter()
	if err != nil {
		log.Fatalf("Failed to create router: %v", err)
	}

	// Initialize core services for managing various resources.
	changelogService := core.NewChangelogService(db)
	domainService := core.NewDomainService(db)
	countryService := core.NewCountryService(db)
	campaignService := core.NewCampaignService(db)
	metricService := core.NewMetricService(db)

	// Message for the / endpoint.
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Api docs can be found at http://ipv6.fail/")
	})

	// Register API endpoints with their respective handlers.
	router.Mount("/domain", rest.DomainHandler{Repo: domainService}.Routes())
	router.Mount("/country", rest.CountryHandler{Repo: countryService}.Routes())
	router.Mount("/changelog", rest.ChangelogHandler{Repo: changelogService}.Routes())
	router.Mount("/campaign", rest.CampaignHandler{Repo: campaignService}.Routes())
	router.Mount("/metric", rest.MetricHandler{Repo: metricService}.Routes())

	// Print the registered routes for debugging purposes.
	rest.PrintRoutes(router)

	// Start the API server with the configured listening address.
	listenAddr := fmt.Sprintf("[::1]:%v", cfg.APIPort)
	log.Printf("Starting server on %s", listenAddr)
	log.Fatal(http.ListenAndServe(listenAddr, router))
}
