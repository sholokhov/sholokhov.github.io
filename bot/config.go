package main

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Telegram  TelegramConfig  `yaml:"telegram"`
	B2        B2Config        `yaml:"b2"`
	GitHub    GitHubConfig    `yaml:"github"`
	Tags      TagsConfig      `yaml:"tags"`
	Thumbnail ThumbnailConfig `yaml:"thumbnail"`
}

type TelegramConfig struct {
	Token         string `yaml:"token"`
	AllowedUserID int64  `yaml:"allowed_user_id"`
}

type B2Config struct {
	KeyID     string `yaml:"key_id"`
	AppKey    string `yaml:"app_key"`
	Bucket    string `yaml:"bucket"`
	Endpoint  string `yaml:"endpoint"`
	PublicURL string `yaml:"public_url"`
}

type GitHubConfig struct {
	Token    string `yaml:"token"`
	Repo     string `yaml:"repo"`
	FilePath string `yaml:"file_path"`
	Branch   string `yaml:"branch"`
}

type TagsConfig struct {
	Locations []string `yaml:"locations"`
	Cameras   []string `yaml:"cameras"`
}

type ThumbnailConfig struct {
	Width   int `yaml:"width"`
	Quality int `yaml:"quality"`
}

func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		// Defaults
		GitHub: GitHubConfig{
			FilePath: "data/photos.json",
			Branch:   "main",
		},
		Thumbnail: ThumbnailConfig{
			Width:   600,
			Quality: 85,
		},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Env var overrides for secrets
	for key, dst := range map[string]*string{
		"TELEGRAM_TOKEN": &cfg.Telegram.Token,
		"B2_KEY_ID":      &cfg.B2.KeyID,
		"B2_APP_KEY":     &cfg.B2.AppKey,
		"B2_BUCKET":      &cfg.B2.Bucket,
		"B2_ENDPOINT":    &cfg.B2.Endpoint,
		"B2_PUBLIC_URL":  &cfg.B2.PublicURL,
		"GITHUB_TOKEN":   &cfg.GitHub.Token,
		"GITHUB_REPO":    &cfg.GitHub.Repo,
	} {
		if v := os.Getenv(key); v != "" {
			*dst = v
		}
	}
	if v := os.Getenv("TELEGRAM_ALLOWED_USER_ID"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.Telegram.AllowedUserID = id
		}
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if c.Telegram.Token == "" {
		return fmt.Errorf("telegram.token is required")
	}
	if c.Telegram.AllowedUserID == 0 {
		return fmt.Errorf("telegram.allowed_user_id is required")
	}
	if c.B2.KeyID == "" || c.B2.AppKey == "" {
		return fmt.Errorf("b2.key_id and b2.app_key are required")
	}
	if c.B2.Bucket == "" || c.B2.Endpoint == "" || c.B2.PublicURL == "" {
		return fmt.Errorf("b2.bucket, b2.endpoint, and b2.public_url are required")
	}
	if c.GitHub.Token == "" || c.GitHub.Repo == "" {
		return fmt.Errorf("github.token and github.repo are required")
	}
	return nil
}
