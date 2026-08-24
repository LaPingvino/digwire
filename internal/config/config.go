package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type SearchProviderConfig struct {
	Name    string  `yaml:"name" json:"name"`
	Type    string  `yaml:"type" json:"type"` // "torrentscsv", "torznab", "archiveorg"
	URL     string  `yaml:"url" json:"url"`
	APIKey  string  `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	Enabled bool    `yaml:"enabled" json:"enabled"`
	Weight  float64 `yaml:"weight" json:"weight"` // 0.1 to 2.0 bias multiplier
}

type Config struct {
	DownloadDir      string                 `yaml:"download_dir" json:"download_dir"`
	ListenPort       int                    `yaml:"listen_port" json:"listen_port"`
	WebPort          int                    `yaml:"web_port" json:"web_port"`
	DownloadLimitKB  int                    `yaml:"download_limit_kb" json:"download_limit_kb"`
	UploadLimitKB    int                    `yaml:"upload_limit_kb" json:"upload_limit_kb"`
	EnableDHT        bool                   `yaml:"enable_dht" json:"enable_dht"`
	EnableUPnP       bool                   `yaml:"enable_upnp" json:"enable_upnp"`
	SearchProviders  []SearchProviderConfig `yaml:"search_providers" json:"search_providers"`
	configPath       string                 `yaml:"-" json:"-"`
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}
	defaultDownloadDir := filepath.Join(home, "Downloads", "Digwire")

	return &Config{
		DownloadDir:     defaultDownloadDir,
		ListenPort:      50007,
		WebPort:         9091,
		DownloadLimitKB: 0, // unlimited
		UploadLimitKB:   0, // unlimited
		EnableDHT:       true,
		EnableUPnP:      true,
		SearchProviders: []SearchProviderConfig{
			{
				Name:    "TorrentsCSV",
				Type:    "torrentscsv",
				URL:     "https://torrents-csv.com",
				Enabled: true,
				Weight:  1.2,
			},
			{
				Name:    "Archive.org",
				Type:    "archiveorg",
				URL:     "https://archive.org",
				Enabled: true,
				Weight:  0.6,
			},
			{
				Name:    "Jackett / Prowlarr (Torznab)",
				Type:    "torznab",
				URL:     "http://localhost:9696/api/v1/search",
				APIKey:  "",
				Enabled: false,
				Weight:  1.0,
			},
		},
	}
}

func GetConfigPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}
	return filepath.Join(configDir, "digwire", "config.yaml")
}

func LoadConfig() (*Config, error) {
	cfgPath := GetConfigPath()
	cfg := DefaultConfig()
	cfg.configPath = cfgPath

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			_ = cfg.Save()
			return cfg, nil
		}
		return cfg, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return cfg, err
	}

	for i := range cfg.SearchProviders {
		if cfg.SearchProviders[i].Weight <= 0 {
			cfg.SearchProviders[i].Weight = 1.0
		}
	}

	cfg.configPath = cfgPath
	return cfg, nil
}

func (c *Config) Save() error {
	if c.configPath == "" {
		c.configPath = GetConfigPath()
	}

	dir := filepath.Dir(c.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(c.configPath, data, 0644)
}
