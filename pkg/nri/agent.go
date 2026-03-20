package nri

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

const (
	// DevicePath is the base path for infiniband device nodes.
	DevicePath = "/dev/infiniband"
)

var (
	// GlobalDevices are infiniband devices shared by all NICs.
	GlobalDevices = []string{
		"rdma_cm",
	}
	// PairDevices are per-NIC infiniband devices (appended with device index).
	PairDevices = []string{
		"uverbs",
		"issm",
		"umad",
	}
)

// Config holds configuration for the NRI plugin.
type Config struct {
	Debug             bool
	Index             int
	Name              string
	Devices           []string // device indices, auto-discovered if empty
	GPURDMAAutoInject bool
}

// RdmaDeviceInjector is the interface for NRI-based device injection.
type RdmaDeviceInjector interface {
	Init(*Config) error
	Run() error
}

// Start discovers RDMA devices and runs the NRI plugin.
func Start(c *Config) error {
	if len(c.Devices) == 0 {
		c.Devices = getAllDeviceIndices()
	}

	if len(c.Devices) == 0 {
		return fmt.Errorf("no RDMA devices found at %s", DevicePath)
	}

	logrus.Infof("NRI plugin config: %+v", c)

	agent := NewNativeAgent()
	if err := agent.Init(c); err != nil {
		return fmt.Errorf("failed to init NRI agent: %w", err)
	}
	return agent.Run()
}

// generateDevicePaths returns all /dev/infiniband/* paths for the given device indices.
func generateDevicePaths(indices []string) []string {
	var paths []string

	for _, key := range GlobalDevices {
		paths = append(paths, fmt.Sprintf("%s/%s", DevicePath, key))
	}
	for _, key := range PairDevices {
		for _, idx := range indices {
			paths = append(paths, fmt.Sprintf("%s/%s%s", DevicePath, key, idx))
		}
	}
	return paths
}

// getAllDeviceIndices returns all RDMA device indices by listing /dev/infiniband/uverbs*.
func getAllDeviceIndices() []string {
	files, err := filepath.Glob(fmt.Sprintf("%s/uverbs*", DevicePath))
	if err != nil {
		return nil
	}

	var indices []string
	for _, file := range files {
		parts := strings.Split(filepath.Base(file), "uverbs")
		if len(parts) >= 2 && parts[1] != "" {
			indices = append(indices, parts[1])
		}
	}
	return indices
}
