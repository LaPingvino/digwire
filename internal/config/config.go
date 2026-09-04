package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type SearchProviderConfig struct {
	Name    string  `yaml:"name" json:"name"`
	Type    string  `yaml:"type" json:"type"` // "btdig", "bitsearch", "apibay", "eztv", "yts", "solidtorrents", "torrentscsv", "limetorrents", "torlock", "archiveorg", "torznab", "generic_json", "generic_html"
	URL     string  `yaml:"url" json:"url"`
	APIKey  string  `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	Enabled bool    `yaml:"enabled" json:"enabled"`
	Weight  float64 `yaml:"weight" json:"weight"` // 0.1 to 2.0 bias multiplier

	// Generic JSON paths for custom API trackers
	ResultsPath string `yaml:"results_path,omitempty" json:"results_path,omitempty"`
	TitlePath   string `yaml:"title_path,omitempty" json:"title_path,omitempty"`
	HashPath    string `yaml:"hash_path,omitempty" json:"hash_path,omitempty"`
	MagnetPath  string `yaml:"magnet_path,omitempty" json:"magnet_path,omitempty"`
	SizePath    string `yaml:"size_path,omitempty" json:"size_path,omitempty"`
	SeedsPath   string `yaml:"seeds_path,omitempty" json:"seeds_path,omitempty"`
	PeersPath   string `yaml:"peers_path,omitempty" json:"peers_path,omitempty"`

	// Generic regex patterns for custom HTML scrapers
	RowRegex    string `yaml:"row_regex,omitempty" json:"row_regex,omitempty"`
	TitleRegex  string `yaml:"title_regex,omitempty" json:"title_regex,omitempty"`
	MagnetRegex string `yaml:"magnet_regex,omitempty" json:"magnet_regex,omitempty"`
	SizeRegex   string `yaml:"size_regex,omitempty" json:"size_regex,omitempty"`
	SeedsRegex  string `yaml:"seeds_regex,omitempty" json:"seeds_regex,omitempty"`
}

type Config struct {
	DownloadDir     string                 `yaml:"download_dir" json:"download_dir"`
	ListenPort      int                    `yaml:"listen_port" json:"listen_port"`
	WebPort         int                    `yaml:"web_port" json:"web_port"`
	DownloadLimitKB int                    `yaml:"download_limit_kb" json:"download_limit_kb"`
	UploadLimitKB   int                    `yaml:"upload_limit_kb" json:"upload_limit_kb"`
	EnableDHT       bool                   `yaml:"enable_dht" json:"enable_dht"`
	EnableUPnP      bool                   `yaml:"enable_upnp" json:"enable_upnp"`
	FallbackDNS     []string               `yaml:"fallback_dns" json:"fallback_dns"`
	AutoPreseedDHT  bool                   `yaml:"auto_preseed_dht" json:"auto_preseed_dht"`
	DHTCrawlWorkers int                    `yaml:"dht_crawl_workers" json:"dht_crawl_workers"`
	GermanyMode     bool                   `yaml:"germany_mode" json:"germany_mode"`
	SearchProviders []SearchProviderConfig `yaml:"search_providers" json:"search_providers"`
	configPath      string                 `yaml:"-" json:"-"`
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
		FallbackDNS: []string{
			"8.8.8.8:53",
			"1.1.1.1:53",
			"8.8.4.4:53",
			"1.0.0.1:53",
			"9.9.9.9:53",
		},
		AutoPreseedDHT:  true,
		DHTCrawlWorkers: 8,
		GermanyMode:     false,
		SearchProviders: []SearchProviderConfig{
			{
				Name:    "BTDigg (DHT)",
				Type:    "btdig",
				URL:     "https://btdig.com",
				Enabled: true,
				Weight:  1.4,
			},
			{
				Name:    "BitSearch (DHT)",
				Type:    "bitsearch",
				URL:     "https://bitsearch.to",
				Enabled: true,
				Weight:  1.4,
			},
			{
				Name:    "The Pirate Bay",
				Type:    "apibay",
				URL:     "https://apibay.org",
				Enabled: true,
				Weight:  1.3,
			},
			{
				Name:    "EZTV (TV Shows)",
				Type:    "eztv",
				URL:     "https://eztv.re",
				Enabled: true,
				Weight:  1.2,
			},
			{
				Name:    "YTS (HD Movies)",
				Type:    "yts",
				URL:     "https://yts.mx",
				Enabled: true,
				Weight:  1.2,
			},
			{
				Name:    "SolidTorrents",
				Type:    "solidtorrents",
				URL:     "https://solidtorrents.to",
				Enabled: true,
				Weight:  1.1,
			},
			{
				Name:    "TorrentsCSV",
				Type:    "torrentscsv",
				URL:     "https://torrents-csv.com",
				Enabled: true,
				Weight:  1.0,
			},
			{
				Name:    "LimeTorrents",
				Type:    "limetorrents",
				URL:     "https://www.limetorrents.lol",
				Enabled: true,
				Weight:  0.9,
			},
			{
				Name:    "TorLock",
				Type:    "torlock",
				URL:     "https://www.torlock.com",
				Enabled: true,
				Weight:  0.9,
			},
			{
				Name:    "Archive.org",
				Type:    "archiveorg",
				URL:     "https://archive.org",
				Enabled: true,
				Weight:  0.6,
			},
			{
				Name:    "Soulseek P2P (Music & Lossless Audio)",
				Type:    "soulseek",
				URL:     "https://archive.org",
				Enabled: true,
				Weight:  1.5,
			},
			{
				Name:    "Library & Documents (Scribd / Books)",
				Type:    "documents",
				URL:     "https://archive.org",
				Enabled: true,
				Weight:  1.3,
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
	defCfg := DefaultConfig()
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

	changed := false

	if cfg.WebPort <= 0 {
		cfg.WebPort = defCfg.WebPort
		changed = true
	}
	if cfg.ListenPort <= 0 {
		cfg.ListenPort = defCfg.ListenPort
		changed = true
	}
	if cfg.DownloadDir == "" || filepath.IsAbs(cfg.DownloadDir) && (len(cfg.DownloadDir) > 4 && cfg.DownloadDir[:4] == "/tmp") {
		cfg.DownloadDir = defCfg.DownloadDir
		changed = true
	}

	// Ensure fallback DNS is populated
	if len(cfg.FallbackDNS) == 0 {
		cfg.FallbackDNS = defCfg.FallbackDNS
		changed = true
	}

	// Merge any newly introduced default search providers if not present
	existingMap := make(map[string]bool)
	for _, p := range cfg.SearchProviders {
		existingMap[p.Type] = true
		existingMap[p.Name] = true
	}

	for _, defP := range defCfg.SearchProviders {
		if !existingMap[defP.Type] && !existingMap[defP.Name] {
			cfg.SearchProviders = append(cfg.SearchProviders, defP)
			existingMap[defP.Type] = true
			existingMap[defP.Name] = true
			changed = true
		}
	}

	for i := range cfg.SearchProviders {
		if cfg.SearchProviders[i].Weight <= 0 {
			cfg.SearchProviders[i].Weight = 1.0
		}
	}

	cfg.configPath = cfgPath
	if changed {
		_ = cfg.Save()
	}

	return cfg, nil
}

func (c *Config) SetConfigPath(p string) {
	c.configPath = p
}

func (c *Config) GetConfigPath() string {
	if c == nil {
		return ""
	}
	return c.configPath
}

func (c *Config) Save() error {
	if c.WebPort <= 0 {
		c.WebPort = 9091
	}
	if c.ListenPort <= 0 {
		c.ListenPort = 50007
	}
	if c.DownloadDir == "" {
		home, _ := os.UserHomeDir()
		if home == "" {
			home = "."
		}
		c.DownloadDir = filepath.Join(home, "Downloads", "Digwire")
	}

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
