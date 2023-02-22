package config

import (
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

// Read reads the configuration from the config file.
func Read() (*Config, error) {
	viper.AutomaticEnv()
	viper.SetConfigName("app")
	viper.SetConfigType("env")
	viper.AddConfigPath(".")
	viper.AddConfigPath("$HOME")
	if err := viper.ReadInConfig(); err != nil { // Handle errors reading the config file
		return nil, err
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil { // Handle errors reading the config file
		return nil, err
	}
	return &config, nil
}
