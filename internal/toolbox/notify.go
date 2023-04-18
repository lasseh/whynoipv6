package toolbox

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"time"
	"whynoipv6/internal/config"
)

// apiMessage is the message from api to irc
type apiMessage struct {
	Channel string `json:"channel"`
	Message string `json:"message"`
}

var (
	httpClient *http.Client
	cfg        *config.Config
	err        error
)

func init() {
	httpClient = createHTTPClient()

	// Read the configuration.
	cfg, err = config.Read()
	if err != nil {
		log.Fatal("Failed to read config: ", err)
	}
}

// createHTTPClient initializes an http.Client with better default settings.
func createHTTPClient() *http.Client {
	var netTransport = &http.Transport{
		Dial: (&net.Dialer{
			Timeout: 5 * time.Second,
		}).Dial,
		TLSHandshakeTimeout: 5 * time.Second,
		TLSClientConfig:     &tls.Config{},
		Proxy:               http.ProxyFromEnvironment,
	}
	client := &http.Client{
		Timeout:   time.Duration(5) * time.Second,
		Transport: netTransport,
	}
	return client
}

// NotifyIrc sends message to irc
// This is a private setup, please don't use this
func (s *Service) NotifyIrc(m string) {
	// New message
	message := apiMessage{
		Channel: "legz",
		Message: m,
	}
	mJSON, err := json.Marshal(message)
	if err != nil {
		log.Println(err)
	}
	req, err := http.NewRequest("POST", "https://partyvan.lasse.cloud/say", bytes.NewBuffer(mJSON))
	if err != nil {
		log.Println(err)
	}

	// Create a Bearer token
	var bearer = "Bearer " + cfg.IRCToken
	req.Header.Add("Authorization", bearer)

	// Send request
	_, err = httpClient.Do(req)
	if err != nil {
		log.Println(err)
	}

}
