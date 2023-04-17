package config

import (
	"errors"
	"log"

	"github.com/spf13/viper"
)

// Config represents the configuration of the application read from app.env.
type Config struct {
	DatabaseSource      string `mapstructure:"DB_SOURCE"`
	APIPort             string `mapstructure:"API_PORT"`
	IRCToken            string `mapstructure:"IRC_TOKEN"`
	GeoIPPath           string `mapstructure:"GEOIP_PATH"`
	Nameserver          string `mapstructure:"NAMESERVER"`
	HealthcheckCrawler  string `mapstructure:"HEALTHCHECK_CRAWLER"`
	HealthcheckCampaign string `mapstructure:"HEALTHCHECK_CAMPAIGN"`
}

// Read reads the configuration from the app.env file.
func Read() (*Config, error) {
	// Set up Viper to read environment variables.
	viper.AutomaticEnv()

	// Configure Viper to read the app.env file.
	viper.SetConfigName("app")
	viper.SetConfigType("env")
	viper.AddConfigPath(".")
	viper.AddConfigPath("$HOME")

	// Read the app.env file.
	if err := viper.ReadInConfig(); err != nil {
		log.Printf("Error reading config file: %v", err)
		return nil, errors.New("error reading config file")

	}

	// Unmarshal the configuration into a Config struct.
	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		log.Printf("Error unmarshalling config: %v", err)
		return nil, errors.New("error unmarshalling config")
	}

	return &config, nil
}
