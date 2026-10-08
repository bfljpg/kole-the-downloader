package config

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"

	"github.com/govdbot/govd/internal/logger"
	"gopkg.in/yaml.v2"
)

const configPath = "private/config.yaml"

var extractorConfigs map[string]*ExtractorConfig

func loadFromConfig() {
	extractorConfigs = make(map[string]*ExtractorConfig)

	_, err := os.Stat(configPath)
	if os.IsNotExist(err) {
		return
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		logger.L.Fatalf("failed reading config file: %v", err)
	}

	var rawConfig map[string]*ExtractorConfig

	if err := yaml.Unmarshal(data, &rawConfig); err != nil {
		logger.L.Fatalf("failed parsing config file: %v", err)
	}
	maps.Copy(extractorConfigs, rawConfig)

	validateConfig()
}

func validateConfig() {
	for id, cfg := range extractorConfigs {
		var active int
		if cfg.Proxy != "" {
			active++
		}
		if cfg.EdgeProxy != "" {
			active++
		}
		if cfg.DisableProxy {
			active++
		}
		if active > 1 {
			logger.L.Fatalf("[%s] invalid config: cannot enable more than one proxy option at the same time", id)
		}
		if err := validateSessionProxy(cfg.SessionProxy); err != nil {
			logger.L.Fatalf("[%s] invalid config: session_proxy: %v", id, err)
		}
		if len(cfg.Instance) > 0 && id != "youtube" {
			logger.L.Fatalf("[%s] invalid config: custom instance is only supported for youtube extractor", id)
		}
		for _, r := range cfg.IgnoreRegex {
			if r == nil {
				logger.L.Fatalf("[%s] invalid config: ignore_regex contains invalid regex", id)
			}
		}
	}
}

// validateSessionProxy makes sure a session proxy is usable, as a bad
// url must never make session requests fall back to a direct connection.
func validateSessionProxy(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return fmt.Errorf("unsupported scheme %q (use http, https, socks5 or socks5h)", u.Scheme)
	}
	if u.Hostname() == "" || u.Port() == "" {
		return errors.New("host and port are required")
	}
	return nil
}

func GetExtractorConfig(extractorID string) *ExtractorConfig {
	if config, exists := extractorConfigs[extractorID]; exists {
		return config
	}
	return &ExtractorConfig{}
}
