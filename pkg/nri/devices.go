package nri

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/containerd/nri/pkg/api"
	nrilog "github.com/containerd/nri/pkg/log"
	"github.com/containerd/nri/pkg/stub"
	"github.com/sirupsen/logrus"
	"sigs.k8s.io/yaml"
)

const (
	// Annotation key prefixes for NRI device/mount/CDI injection.
	deviceKey    = "devices.nri.io"
	mountKey     = "mounts.nri.io"
	cdiDeviceKey = "cdi-devices.nri.io"
)

var nriLog = nrilog.Get()

// device represents an annotated device to inject.
type device struct {
	Path     string `json:"path"`
	Type     string `json:"type"`
	Major    int64  `json:"major"`
	Minor    int64  `json:"minor"`
	FileMode uint32 `json:"file_mode"`
	UID      uint32 `json:"uid"`
	GID      uint32 `json:"gid"`
}

// mount represents an annotated mount to inject.
type mount struct {
	Source      string   `json:"source"`
	Destination string   `json:"destination"`
	Type        string   `json:"type"`
	Options     []string `json:"options"`
}

// nativeAgent implements the NRI plugin using the containerd NRI stub.
type nativeAgent struct {
	*Config
	stub           stub.Stub
	defaultDevices []device
}

// NewNativeAgent creates a new in-process NRI agent.
func NewNativeAgent() RdmaDeviceInjector {
	return &nativeAgent{}
}

func (na *nativeAgent) Init(c *Config) error {
	na.Config = c

	// Initialize default devices from discovered device paths
	paths := generateDevicePaths(c.Devices)
	na.initDefaultDevices(paths)
	return nil
}

func (na *nativeAgent) Run() error {
	var opts []stub.Option

	if na.Name != "" {
		opts = append(opts, stub.WithPluginName(na.Name))
	}
	if na.Index > 0 {
		opts = append(opts, stub.WithPluginIdx(fmt.Sprintf("%d", na.Index)))
	}

	var err error
	na.stub, err = stub.New(na, opts...)
	if err != nil {
		return fmt.Errorf("failed to create NRI stub: %w", err)
	}

	logrus.Infof("starting NRI device injector plugin (name=%s, idx=%d)", na.Name, na.Index)

	if err := na.stub.Run(context.Background()); err != nil {
		return fmt.Errorf("NRI plugin exited: %w", err)
	}
	return nil
}

// CreateContainer is the NRI hook called when a new container is created.
func (na *nativeAgent) CreateContainer(ctx context.Context, pod *api.PodSandbox, ctr *api.Container) (*api.ContainerAdjustment, []*api.ContainerUpdate, error) {
	if na.Debug {
		dump(ctx, "CreateContainer", "pod", pod, "container", ctr)
	}

	adjust := &api.ContainerAdjustment{}

	// 1. Inject default RDMA devices + annotation-based devices
	if err := na.injectDevices(ctx, pod, ctr, adjust); err != nil {
		return nil, nil, err
	}

	// 2. GPU-RDMA auto-injection based on PCIe affinity
	na.injectGPURDMADevices(ctx, pod, ctr, adjust)

	// 3. CDI device injection from annotations
	if err := injectCDIDevices(ctx, pod, ctr, adjust, na.Debug); err != nil {
		return nil, nil, err
	}

	// 4. Mount injection from annotations
	if err := injectMounts(ctx, pod, ctr, adjust, na.Debug); err != nil {
		return nil, nil, err
	}

	return adjust, nil, nil
}

// injectGPURDMADevices detects GPU containers via NVIDIA_VISIBLE_DEVICES and
// automatically injects RDMA devices sharing PCIe affinity with the assigned GPUs.
func (na *nativeAgent) injectGPURDMADevices(ctx context.Context, pod *api.PodSandbox, ctr *api.Container, a *api.ContainerAdjustment) {
	if !na.GPURDMAAutoInject {
		return
	}

	nvidiaEnv := getEnvValue(ctr.Env, "NVIDIA_VISIBLE_DEVICES")
	if nvidiaEnv == "" {
		return
	}

	gpuIndices := parseNvidiaVisibleDevices(nvidiaEnv)
	if len(gpuIndices) == 0 {
		return
	}

	nriLog.Infof(ctx, "%s: GPU container detected (NVIDIA_VISIBLE_DEVICES=%s), discovering RDMA affinity...",
		containerName(pod, ctr), nvidiaEnv)

	devicePaths := discoverGPURDMADevices(gpuIndices)
	if len(devicePaths) == 0 {
		nriLog.Infof(ctx, "%s: no RDMA devices found with GPU affinity", containerName(pod, ctr))
		return
	}

	injected := na.statAndInjectDevices(ctx, pod, ctr, a, devicePaths)
	nriLog.Infof(ctx, "%s: injected %d RDMA devices for GPU affinity: %v",
		containerName(pod, ctr), injected, devicePaths)
}

// statAndInjectDevices stats each device path and injects it into the container adjustment.
func (na *nativeAgent) statAndInjectDevices(ctx context.Context, pod *api.PodSandbox, ctr *api.Container, a *api.ContainerAdjustment, devicePaths []string) int {
	injected := 0
	for _, devPath := range devicePaths {
		info, err := os.Stat(devPath)
		if err != nil {
			nriLog.Errorf(ctx, "%s: failed to stat device %s: %v", containerName(pod, ctr), devPath, err)
			continue
		}

		d := device{Path: devPath}
		if info.Mode()&os.ModeCharDevice != 0 {
			d.Type = "c"
		} else if info.Mode()&os.ModeDevice != 0 {
			d.Type = "b"
		} else {
			nriLog.Errorf(ctx, "%s: %s is not a device node", containerName(pod, ctr), devPath)
			continue
		}

		stat := info.Sys().(*syscall.Stat_t)
		d.Major = int64(stat.Rdev >> 8)
		d.Minor = int64(stat.Rdev & 0xff)
		d.FileMode = uint32(info.Mode())
		d.UID = stat.Uid
		d.GID = stat.Gid

		a.AddDevice(d.toNRI())
		injected++

		if na.Debug {
			nriLog.Infof(ctx, "%s: injected GPU-affinity RDMA device %s (major=%d, minor=%d)",
				containerName(pod, ctr), devPath, d.Major, d.Minor)
		}
	}
	return injected
}

// injectDevices injects the default RDMA devices plus any annotation-based devices.
func (na *nativeAgent) injectDevices(ctx context.Context, pod *api.PodSandbox, ctr *api.Container, a *api.ContainerAdjustment) error {
	annotatedDevices, err := parseDevices(ctr.Name, pod.Annotations)
	if err != nil {
		return err
	}

	toInject := combineDevices(na.defaultDevices, annotatedDevices)
	if len(toInject) == 0 {
		nriLog.Infof(ctx, "%s: no RDMA devices to inject", containerName(pod, ctr))
		return nil
	}

	if na.Debug {
		dump(ctx, containerName(pod, ctr), "devices to inject", toInject)
	}

	for _, d := range toInject {
		a.AddDevice(d.toNRI())
		nriLog.Infof(ctx, "%s: injected device %q", containerName(pod, ctr), d.Path)
	}
	return nil
}

// initDefaultDevices stats all discovered device paths and builds the default device list.
func (na *nativeAgent) initDefaultDevices(devicePaths []string) {
	na.defaultDevices = make([]device, 0, len(devicePaths))
	ctx := context.Background()

	for _, devPath := range devicePaths {
		info, err := os.Stat(devPath)
		if err != nil {
			nriLog.Errorf(ctx, "failed to stat default device %s: %v", devPath, err)
			continue
		}

		d := device{Path: devPath}
		if info.Mode()&os.ModeCharDevice != 0 {
			d.Type = "c"
		} else if info.Mode()&os.ModeDevice != 0 {
			d.Type = "b"
		} else {
			nriLog.Errorf(ctx, "%s is not a device node", devPath)
			continue
		}

		stat := info.Sys().(*syscall.Stat_t)
		d.Major = int64(stat.Rdev >> 8)
		d.Minor = int64(stat.Rdev & 0xff)
		d.FileMode = uint32(info.Mode())
		d.UID = stat.Uid
		d.GID = stat.Gid

		na.defaultDevices = append(na.defaultDevices, d)
		if na.Debug {
			nriLog.Infof(ctx, "default device: %+v", d)
		}
	}
}

// --- Annotation parsing ---

func parseDevices(ctr string, annotations map[string]string) ([]device, error) {
	annotation := getAnnotation(annotations, deviceKey, ctr)
	if annotation == nil {
		return nil, nil
	}
	var devices []device
	if err := yaml.Unmarshal(annotation, &devices); err != nil {
		return nil, fmt.Errorf("invalid device annotation %q: %w", string(annotation), err)
	}
	return devices, nil
}

func injectCDIDevices(ctx context.Context, pod *api.PodSandbox, ctr *api.Container, a *api.ContainerAdjustment, verbose bool) error {
	annotation := getAnnotation(pod.Annotations, cdiDeviceKey, ctr.Name)
	if annotation == nil {
		return nil
	}
	var cdiDevices []string
	if err := yaml.Unmarshal(annotation, &cdiDevices); err != nil {
		return fmt.Errorf("invalid CDI device annotation %q: %w", string(annotation), err)
	}
	for _, name := range cdiDevices {
		a.AddCDIDevice(&api.CDIDevice{Name: name})
		if verbose {
			nriLog.Infof(ctx, "%s: injected CDI device %q", containerName(pod, ctr), name)
		}
	}
	return nil
}

func injectMounts(ctx context.Context, pod *api.PodSandbox, ctr *api.Container, a *api.ContainerAdjustment, verbose bool) error {
	annotation := getAnnotation(pod.Annotations, mountKey, ctr.Name)
	if annotation == nil {
		return nil
	}
	var mounts []mount
	if err := yaml.Unmarshal(annotation, &mounts); err != nil {
		return fmt.Errorf("invalid mount annotation %q: %w", string(annotation), err)
	}
	for _, m := range mounts {
		a.AddMount(m.toNRI())
		if verbose {
			nriLog.Infof(ctx, "%s: injected mount %s → %s", containerName(pod, ctr), m.Source, m.Destination)
		}
	}
	return nil
}

// --- Helpers ---

func getAnnotation(annotations map[string]string, mainKey, ctr string) []byte {
	for _, key := range []string{
		mainKey + "/container." + ctr,
		mainKey + "/pod",
		mainKey,
	} {
		if value, ok := annotations[key]; ok {
			return []byte(value)
		}
	}
	return nil
}

func (d *device) toNRI() *api.LinuxDevice {
	dev := &api.LinuxDevice{
		Path:  d.Path,
		Type:  d.Type,
		Major: d.Major,
		Minor: d.Minor,
	}
	if d.FileMode != 0 {
		dev.FileMode = api.FileMode(d.FileMode)
	}
	if d.UID != 0 {
		dev.Uid = api.UInt32(d.UID)
	}
	if d.GID != 0 {
		dev.Gid = api.UInt32(d.GID)
	}
	return dev
}

func (m *mount) toNRI() *api.Mount {
	return &api.Mount{
		Source:      m.Source,
		Destination: m.Destination,
		Type:        m.Type,
		Options:     m.Options,
	}
}

func containerName(pod *api.PodSandbox, ctr *api.Container) string {
	if pod != nil {
		return pod.Name + "/" + ctr.Name
	}
	return ctr.Name
}

// combineDevices merges old and new devices, deduplicating by path.
func combineDevices(old, new []device) []device {
	result := make([]device, len(old))
	copy(result, old)
	for _, nd := range new {
		found := false
		for _, od := range old {
			if nd.Path == od.Path {
				found = true
				break
			}
		}
		if !found {
			result = append(result, nd)
		}
	}
	return result
}

func getEnvValue(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):]
		}
	}
	return ""
}

func dump(ctx context.Context, args ...interface{}) {
	var prefix string
	idx := 0
	if len(args)%2 == 1 {
		prefix = args[0].(string)
		idx++
	}
	for ; idx < len(args)-1; idx += 2 {
		tag, obj := args[idx], args[idx+1]
		msg, err := yaml.Marshal(obj)
		if err != nil {
			nriLog.Infof(ctx, "%s: %s: dump error: %v", prefix, tag, err)
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(msg)), "\n") {
			if prefix != "" {
				nriLog.Infof(ctx, "%s: %s: %s", prefix, tag, line)
			} else {
				nriLog.Infof(ctx, "%s: %s", tag, line)
			}
		}
	}
}
