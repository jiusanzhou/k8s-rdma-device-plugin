package nri

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

var (
	// Sysfs paths for GPU and RDMA device enumeration.
	SysNvidiaDriverDir = "/sys/bus/pci/drivers/nvidia"
	SysInfinibandDir   = "/sys/class/infiniband"
	SysPciDevDir       = "/sys/bus/pci/devices"

	pciBDFRegex   = regexp.MustCompile(`^([0-9a-f]{4}:)?[0-9a-f]{2}:[0-9a-f]{2}\.[0-9a-f]$`)
	pcieRootRegex = regexp.MustCompile(`pci[0-9a-f]{4}:[0-9a-f]{2}`)
)

type gpuDevice struct {
	Index    int
	BusID    string
	NumaNode int
	PCIeRoot string
}

type rdmaDevice struct {
	Name     string // e.g. mlx5_0
	BusID    string
	NumaNode int
	PCIeRoot string
}

// discoverGPURDMADevices finds RDMA devices that have PCIe affinity with the given GPU indices.
// Returns a deduplicated list of /dev/infiniband/* device paths to inject.
func discoverGPURDMADevices(gpuIndices []int) []string {
	gpus := enumerateGPUs()
	if len(gpus) == 0 {
		logrus.Warn("no NVIDIA GPUs found in sysfs")
		return nil
	}

	rdmaDevs := enumerateRDMADevices()
	if len(rdmaDevs) == 0 {
		logrus.Warn("no RDMA devices found in sysfs")
		return nil
	}

	matched := make(map[string]bool)
	for _, gpuIdx := range gpuIndices {
		if gpuIdx < 0 || gpuIdx >= len(gpus) {
			logrus.Warnf("GPU index %d out of range (total %d GPUs)", gpuIdx, len(gpus))
			continue
		}
		gpu := gpus[gpuIdx]
		rdma := findAffinityRDMA(gpu, rdmaDevs)
		if rdma == nil {
			logrus.Warnf("no RDMA device found with affinity to GPU %d (%s)", gpu.Index, gpu.BusID)
			continue
		}
		logrus.Infof("GPU %d (%s) matched RDMA %s (%s) via PCIe/NUMA affinity",
			gpu.Index, gpu.BusID, rdma.Name, rdma.BusID)
		matched[rdma.Name] = true
	}

	return collectDevicePaths(matched)
}

// enumerateGPUs reads /sys/bus/pci/drivers/nvidia/ to get all GPU PCI BDFs, sorted.
// The sort order matches NVIDIA's GPU index enumeration (index 0 = lowest PCI BDF).
func enumerateGPUs() []gpuDevice {
	entries, err := os.ReadDir(SysNvidiaDriverDir)
	if err != nil {
		logrus.Debugf("failed to read nvidia driver dir %s: %v", SysNvidiaDriverDir, err)
		return nil
	}

	var busIDs []string
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if pciBDFRegex.MatchString(name) {
			busIDs = append(busIDs, name)
		}
	}
	sort.Strings(busIDs)

	gpus := make([]gpuDevice, 0, len(busIDs))
	for i, busID := range busIDs {
		gpus = append(gpus, gpuDevice{
			Index:    i,
			BusID:    busID,
			NumaNode: readNumaNode(busID),
			PCIeRoot: readPCIeRoot(busID),
		})
		logrus.Debugf("discovered GPU %d: busID=%s numa=%d pcie=%s",
			i, busID, gpus[i].NumaNode, gpus[i].PCIeRoot)
	}
	return gpus
}

// enumerateRDMADevices reads /sys/class/infiniband/ and resolves each device's PCI topology.
func enumerateRDMADevices() []rdmaDevice {
	entries, err := os.ReadDir(SysInfinibandDir)
	if err != nil {
		logrus.Debugf("failed to read infiniband dir %s: %v", SysInfinibandDir, err)
		return nil
	}

	var devs []rdmaDevice
	for _, entry := range entries {
		name := entry.Name()
		busID := rdmaDeviceToPCIBusID(name)
		if busID == "" {
			continue
		}
		devs = append(devs, rdmaDevice{
			Name:     name,
			BusID:    busID,
			NumaNode: readNumaNode(busID),
			PCIeRoot: readPCIeRoot(busID),
		})
		logrus.Debugf("discovered RDMA %s: busID=%s numa=%d pcie=%s",
			name, busID, devs[len(devs)-1].NumaNode, devs[len(devs)-1].PCIeRoot)
	}
	return devs
}

// findAffinityRDMA picks the best RDMA device for a GPU.
// Priority: same PCIe root complex > same NUMA node > none.
func findAffinityRDMA(gpu gpuDevice, rdmaDevs []rdmaDevice) *rdmaDevice {
	var numaMatch *rdmaDevice
	for i := range rdmaDevs {
		rdma := &rdmaDevs[i]
		if gpu.PCIeRoot != "" && rdma.PCIeRoot != "" && gpu.PCIeRoot == rdma.PCIeRoot {
			return rdma // PCIe root match is best
		}
		if numaMatch == nil && gpu.NumaNode >= 0 && rdma.NumaNode >= 0 && gpu.NumaNode == rdma.NumaNode {
			numaMatch = rdma
		}
	}
	return numaMatch
}

// rdmaDeviceToPCIBusID resolves an RDMA device name (e.g. mlx5_0) to its PCI BDF.
func rdmaDeviceToPCIBusID(rdmaName string) string {
	deviceLink := filepath.Join(SysInfinibandDir, rdmaName, "device")
	resolved, err := filepath.EvalSymlinks(deviceLink)
	if err != nil {
		return ""
	}
	busID := filepath.Base(resolved)
	if !pciBDFRegex.MatchString(busID) {
		return ""
	}
	return busID
}

func readNumaNode(pciBusID string) int {
	data, err := os.ReadFile(filepath.Join(SysPciDevDir, pciBusID, "numa_node"))
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return -1
	}
	return n
}

func readPCIeRoot(pciBusID string) string {
	resolved, err := filepath.EvalSymlinks(filepath.Join(SysPciDevDir, pciBusID))
	if err != nil {
		return ""
	}
	matches := pcieRootRegex.FindAllString(resolved, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

// collectDevicePaths converts matched RDMA device names into /dev/infiniband/* paths.
func collectDevicePaths(matchedRDMA map[string]bool) []string {
	seen := make(map[string]bool)
	var paths []string

	addPath := func(p string) {
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	if len(matchedRDMA) > 0 {
		addPath(fmt.Sprintf("%s/rdma_cm", DevicePath))
	}

	for rdmaName := range matchedRDMA {
		for _, dev := range findCharDevicesForRDMA(rdmaName) {
			addPath(dev)
		}
	}

	sort.Strings(paths)
	return paths
}

// findCharDevicesForRDMA finds /dev/infiniband/{uverbs*,umad*,issm*} for a given RDMA device.
func findCharDevicesForRDMA(rdmaName string) []string {
	var devPaths []string

	sysClasses := []struct {
		dir    string
		prefix string
	}{
		{"/sys/class/infiniband_verbs", "uverbs"},
		{"/sys/class/infiniband_mad", "umad"},
		{"/sys/class/infiniband_mad", "issm"},
	}

	for _, sc := range sysClasses {
		entries, err := os.ReadDir(sc.dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasPrefix(name, sc.prefix) {
				continue
			}
			data, err := os.ReadFile(filepath.Join(sc.dir, name, "ibdev"))
			if err != nil {
				continue
			}
			if strings.TrimSpace(string(data)) == rdmaName {
				devPaths = append(devPaths, fmt.Sprintf("%s/%s", DevicePath, name))
			}
		}
	}
	return devPaths
}

// parseNvidiaVisibleDevices parses the NVIDIA_VISIBLE_DEVICES env var value into GPU indices.
func parseNvidiaVisibleDevices(value string) []int {
	value = strings.TrimSpace(value)
	if value == "" || value == "all" || value == "none" || value == "void" {
		return nil
	}

	parts := strings.Split(value, ",")
	var indices []int
	for _, part := range parts {
		part = strings.TrimSpace(part)
		idx, err := strconv.Atoi(part)
		if err != nil {
			logrus.Debugf("skipping non-numeric GPU identifier: %s", part)
			continue
		}
		indices = append(indices, idx)
	}
	return indices
}
