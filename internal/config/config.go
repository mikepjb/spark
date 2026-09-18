package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

const (
	defaultEndpoint  = "http://127.0.0.1:7777"
	defaultQueueSize = 5
	defaultPath      = ".sparkrc"
)

type Config struct {
	Endpoint     string `yaml:"endpoint"`
	Model        string `yaml:"model"`
	SystemPrompt string `yaml:"system_prompt"`
	APIKey       string `yaml:"-"`
	QueueLimit   int    `yaml:"queue_limit"`
}

func Load() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("find home directory: %w", err)
	}

	return LoadFromFile(home + string(os.PathSeparator) + defaultPath)
}

func LoadFromFile(path string) (Config, error) {
	cfg := Config{
		Endpoint:   defaultEndpoint,
		QueueLimit: defaultQueueSize,
	}

	data, err := os.ReadFile(path)
	if err == nil {
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		if err := decoder.Decode(&cfg); err != nil && err != io.EOF {
			return Config{}, fmt.Errorf("decode config %q: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	if err := applyEnvironment(&cfg); err != nil {
		return Config{}, err
	}
	if cfg.QueueLimit < 1 {
		return Config{}, fmt.Errorf("queue_limit must be at least 1")
	}

	return cfg, nil
}

func applyEnvironment(cfg *Config) error {
	if value, ok := os.LookupEnv("SPARK_ENDPOINT"); ok {
		cfg.Endpoint = value
	}
	if value, ok := os.LookupEnv("SPARK_MODEL"); ok {
		cfg.Model = value
	}
	if value, ok := os.LookupEnv("SPARK_SYSTEM_PROMPT"); ok {
		cfg.SystemPrompt = value
	}
	if value, ok := os.LookupEnv("SPARK_API_KEY"); ok {
		cfg.APIKey = value
	}
	if value, ok := os.LookupEnv("SPARK_QUEUE_LIMIT"); ok {
		limit, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("parse SPARK_QUEUE_LIMIT: %w", err)
		}
		cfg.QueueLimit = limit
	}

	return nil
}
