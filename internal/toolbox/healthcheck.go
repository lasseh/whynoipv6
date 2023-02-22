package toolbox

import (
	"fmt"
	"net/http"
	"time"
)

// HealthCheckUpdate notifys BetterUptime.com of a successfull run
func (s *Service) HealthCheckUpdate(uuid string) {
	var client = &http.Client{
		Timeout: 10 * time.Second,
	}

	_, err := client.Head(fmt.Sprintf("https://betteruptime.com/api/v1/heartbeat/%s", uuid))
	if err != nil {
		fmt.Printf("%s", err)
	}
}
