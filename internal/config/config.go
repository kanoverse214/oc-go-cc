// Package config handles application configuration loading and validation.
package config

// Config holds the complete application configuration.
type Config struct {
	APIKey     string           `json:"api_key"`
	Host       string           `json:"host"`
	Port       int              `json:"port"`
	OpenCodeGo OpenCodeGoConfig `json:"opencode_go"`
	Logging    LoggingConfig    `json:"logging"`
}

// OpenCodeGoConfig holds the upstream OpenCode Go API settings.
type OpenCodeGoConfig struct {
	BaseURL          string `json:"base_url"`
	AnthropicBaseURL string `json:"anthropic_base_url"`
	TimeoutMs        int    `json:"timeout_ms"`
}

// LoggingConfig controls application logging behavior.
type LoggingConfig struct {
	Level    string `json:"level"`
	Requests bool   `json:"requests"`
}
