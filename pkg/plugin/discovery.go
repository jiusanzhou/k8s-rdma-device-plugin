package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	InfinibandDevPath = "/dev/infiniband"
)

// FindRdmaDevice checks if any RDMA devices exist on the node.
func FindRdmaDevice() bool {
	_, err := os.Stat(InfinibandDevPath)
	return err == nil
}

// DiscoverDeviceIndices discovers RDMA device indices by listing /dev/infiniband/uverbs* entries.
func DiscoverDeviceIndices() []string {
	files, err := filepath.Glob(fmt.Sprintf("%s/uverbs*", InfinibandDevPath))
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
