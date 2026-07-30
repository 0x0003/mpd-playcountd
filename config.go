package main

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type config struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	Password string `toml:"password"`
	Retries  int    `toml:"retries"`
}

func defaultConfig() config {
	return config{
		Host:    "127.0.0.1",
		Port:    6600,
		Retries: 1,
	}
}

func configPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "mpd-playcountd", "config.toml")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "mpd-playcountd", "config.toml")
	}
	return ""
}

func loadConfig(path string) config {
	cfg := defaultConfig()
	if path == "" {
		return cfg
	}
	_, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		if !os.IsNotExist(err) {
			logf("config: %v\n", err)
		}
	}
	return cfg
}
