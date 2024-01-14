package toolbox

import (
	"errors"
	"fmt"
	"net/http"
	"time"
	"whynoipv6/internal/logger"
)

// HealthCheckUpdate sends a successful health check notification to BetterUptime.com.
// The function takes a unique identifier (uuid) as input and sends an HTTP HEAD request to BetterUptime.com's API.
// If there's an error, it will log the error message.
func (s *Service) HealthCheckUpdate(uuid string) {
	var log = logger.GetLogger()
	log = log.With().Str("service", "HealthCheckUpdate").Logger()
	// Create an HTTP client with a 10-second timeout.
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Create the URL for the BetterUptime.com API.
	apiURL := fmt.Sprintf("https://betteruptime.com/api/v1/heartbeat/%s", uuid)

	// Send the HTTP HEAD request.
	resp, err := httpClient.Head(apiURL)

	// If there's an error, log the error message.
	if err != nil {
		log.Err(err).Msg("Error while sending health check update.")
		return
	}

	// Close the response body when the function exits.
	defer resp.Body.Close()

	// Check if the response status code indicates success (2xx).
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Debug().Msg("Successfully sent healthcheck update.")
	} else {
		// log.Printf("Failed to send health check update. Status code: %d\n", resp.StatusCode)
		log.Err(errors.New("Status Code:" + resp.Status)).Msg("Failed to send health check update.")
	}
}
