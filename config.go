package main

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/util/yaml"
)

type Config struct {
	Quiet          bool   `yaml:"quiet"`
	NoColor        bool   `yaml:"noColor"`
	Raw            bool   `yaml:"raw"`
	Timestamps     bool   `yaml:"timestamps"`
	ColorMode      string `yaml:"colorMode"`
	ColorScheme    string `yaml:"colorScheme"`
	TemplateString string `yaml:"templateString"`
	KubeConfigPath string `yaml:"kubeConfigPath"`
}

func (c *Config) LoadDefault() error {
	home := os.Getenv("HOME")
	if home == "" {
		return nil
	}
	if err := c.LoadFromPath(filepath.Join(home, ".config", "ktail", "config.yml")); err != nil &&
		!os.IsNotExist(err) {
		return err
	}
	return nil
}

func (c *Config) LoadFromPath(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.UnmarshalStrict(data, c); err != nil {
		return fmt.Errorf("parsing config file %q: %w", path, err)
	}
	return nil
}
