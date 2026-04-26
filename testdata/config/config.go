package config

import "os"

// Config holds application configuration loaded from environment variables.
type Config struct {
	DatabaseUrl    string
	StripeUrl      string
	SendgridUrl    string
	SendgridApiKey string
	GeocodeUrl     string
}

// Load reads configuration from environment variables.
func Load() *Config {
	return &Config{
		DatabaseUrl:    os.Getenv("DATABASE_URL"),
		StripeUrl:      os.Getenv("STRIPE_URL"),
		SendgridUrl:    os.Getenv("SENDGRID_URL"),
		SendgridApiKey: os.Getenv("SENDGRID_API_KEY"),
		GeocodeUrl:     os.Getenv("GEOCODE_URL"),
	}
}
