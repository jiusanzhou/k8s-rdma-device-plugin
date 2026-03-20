package config

import (
	"os"
	"strconv"

	"sigs.k8s.io/yaml"
)

const (
	DefaultResourceName  = "rdma.io/hca"
	DefaultResourceCount = 100
	DefaultNRIPluginName = "rdma-device-plugin"
	DefaultNRIPluginIdx  = 10
	DefaultLogPath       = "/data/log/kubernetes/k8s-rdma-device-plugin.log"
)

// Config holds all configuration for the RDMA device plugin.
type Config struct {
	EnableDevicePlugin bool   `yaml:"enableDevicePlugin" json:"enableDevicePlugin"`
	ResourceName       string `yaml:"resourceName" json:"resourceName"`
	ResourceCount      int    `yaml:"resourceCount" json:"resourceCount"`
	NRIPluginName      string `yaml:"nriPluginName" json:"nriPluginName"`
	NRIPluginIndex     int    `yaml:"nriPluginIndex" json:"nriPluginIndex"`
	GPURDMAAutoInject  bool   `yaml:"gpuRdmaAutoInject" json:"gpuRdmaAutoInject"`
	Debug              bool   `yaml:"debug" json:"debug"`
	LogPath            string `yaml:"logPath" json:"logPath"`
}

// DefaultConfig returns a Config with default values.
func DefaultConfig() *Config {
	return &Config{
		EnableDevicePlugin: true,
		ResourceName:       DefaultResourceName,
		ResourceCount:      DefaultResourceCount,
		NRIPluginName:      DefaultNRIPluginName,
		NRIPluginIndex:     DefaultNRIPluginIdx,
		LogPath:            DefaultLogPath,
	}
}

// LoadFromFile loads config from a YAML file, merging with defaults.
func LoadFromFile(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// ApplyEnvOverrides applies environment variable overrides to the config.
func (c *Config) ApplyEnvOverrides() {
	if v := os.Getenv("RDMA_ENABLE_DEVICE_PLUGIN"); v == "false" || v == "0" {
		c.EnableDevicePlugin = false
	}
	if v := os.Getenv("RDMA_RESOURCE_NAME"); v != "" {
		c.ResourceName = v
	}
	if v := os.Getenv("RDMA_RESOURCE_COUNT"); v != "" {
		if count, err := strconv.Atoi(v); err == nil {
			c.ResourceCount = count
		}
	}
	if v := os.Getenv("RDMA_NRI_PLUGIN_NAME"); v != "" {
		c.NRIPluginName = v
	}
	if v := os.Getenv("RDMA_GPU_AUTO_INJECT"); v == "true" || v == "1" {
		c.GPURDMAAutoInject = true
	}
	if v := os.Getenv("RDMA_DEBUG"); v == "true" || v == "1" {
		c.Debug = true
	}
	if v := os.Getenv("RDMA_LOG_PATH"); v != "" {
		c.LogPath = v
	}
}
