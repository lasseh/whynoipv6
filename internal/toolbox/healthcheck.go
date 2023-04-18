package toolbox

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

// HealthCheckUpdate notifys BetterUptime.com of a successfull run
// func (s *Service) HealthCheckUpdate(uuid string) {
// 	var client = &http.Client{
// 		Timeout: 10 * time.Second,
// 	}

// 	_, err := client.Head(fmt.Sprintf("https://betteruptime.com/api/v1/heartbeat/%s", uuid))
// 	if err != nil {
// 		fmt.Printf("%s", err)
// 	}
// }

// HealthCheckUpdate sends a successful health check notification to BetterUptime.com.
// The function takes a unique identifier (uuid) as input and sends an HTTP HEAD request to BetterUptime.com's API.
// If there's an error, it will log the error message.
func (s *Service) HealthCheckUpdate(uuid string) {
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
		log.Printf("Error while sending health check update: %s\n", err)
		return
	}

	// Close the response body when the function exits.
	defer resp.Body.Close()

	// Check if the response status code indicates success (2xx).
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Println("Successfully sent health check update.")
	} else {
		log.Printf("Failed to send health check update. Status code: %d\n", resp.StatusCode)
	}
}
