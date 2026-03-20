package plugin

import (
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	dp "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	rdmaSocketPath    = "/var/lib/kubelet/device-plugins/rdma.sock"
	kubeletSocketPath = "/var/lib/kubelet/device-plugins/kubelet.sock"
)

// RdmaDevicePlugin implements the kubelet device plugin gRPC interface.
type RdmaDevicePlugin struct {
	devs         []*dp.Device
	socket       string
	resourceName string
	server       *grpc.Server
	stop         chan struct{}
}

// NewRdmaDevicePlugin creates a new RDMA device plugin with the given resource name and count.
func NewRdmaDevicePlugin(resourceName string, resourceCount int) (*RdmaDevicePlugin, error) {
	if resourceName == "" {
		return nil, fmt.Errorf("resource name must not be empty")
	}
	if resourceCount <= 0 {
		return nil, fmt.Errorf("resource count must be > 0, got %d", resourceCount)
	}

	devs := make([]*dp.Device, resourceCount)
	for i := 0; i < resourceCount; i++ {
		devs[i] = &dp.Device{
			ID:     "rdma-" + strconv.Itoa(i),
			Health: dp.Healthy,
		}
	}

	return &RdmaDevicePlugin{
		devs:         devs,
		socket:       rdmaSocketPath,
		resourceName: resourceName,
	}, nil
}

// Serve starts the gRPC server and registers with kubelet.
func (p *RdmaDevicePlugin) Serve() error {
	p.server = grpc.NewServer()
	p.stop = make(chan struct{})

	if err := p.start(); err != nil {
		p.cleanup()
		return fmt.Errorf("could not start device plugin gRPC server: %w", err)
	}

	logrus.Infof("device plugin serving on %s, resource=%s, count=%d", p.socket, p.resourceName, len(p.devs))
	return nil
}

// Stop gracefully shuts down the plugin.
func (p *RdmaDevicePlugin) Stop() {
	if p == nil || p.server == nil {
		return
	}
	p.cleanup()
}

func (p *RdmaDevicePlugin) start() error {
	_ = os.Remove(p.socket)

	sock, err := net.Listen("unix", p.socket)
	if err != nil {
		return fmt.Errorf("could not listen on %s: %w", p.socket, err)
	}

	dp.RegisterDevicePluginServer(p.server, p)

	go func() {
		lastCrash := time.Now()
		restarts := 0
		for {
			logrus.Info("starting gRPC server for device plugin")
			if err := p.server.Serve(sock); err == nil {
				break
			} else {
				logrus.Errorf("device plugin gRPC server crashed: %v", err)
			}

			if restarts > 5 {
				logrus.Fatal("device plugin gRPC server crashed too many times, quitting")
			}
			if time.Since(lastCrash) > time.Hour {
				restarts = 0
			}
			restarts++
			lastCrash = time.Now()
		}
	}()

	// Verify the server is reachable
	conn, err := p.dial(p.socket, 5*time.Second)
	if err != nil {
		return fmt.Errorf("could not verify gRPC server: %w", err)
	}
	conn.Close()
	return nil
}

func (p *RdmaDevicePlugin) cleanup() {
	if p.stop != nil {
		select {
		case <-p.stop:
		default:
			close(p.stop)
		}
	}
	if p.server != nil {
		p.server.Stop()
		p.server = nil
	}
	_ = os.Remove(p.socket)
}

// Register explicitly registers this plugin with the kubelet.
// This is optional — kubelet can also discover it via the socket file.
func (p *RdmaDevicePlugin) Register() error {
	conn, err := p.dial(kubeletSocketPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to kubelet: %w", err)
	}
	defer conn.Close()

	client := dp.NewRegistrationClient(conn)
	_, err = client.Register(context.Background(), &dp.RegisterRequest{
		Version:      dp.Version,
		Endpoint:     path.Base(p.socket),
		ResourceName: p.resourceName,
	})
	return err
}

func (p *RdmaDevicePlugin) dial(unixSocketPath string, timeout time.Duration) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return grpc.DialContext(ctx, "unix://"+unixSocketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
}

// --- DevicePluginServer interface ---

// ListAndWatch sends the list of devices and blocks until stopped.
func (p *RdmaDevicePlugin) ListAndWatch(_ *dp.Empty, s dp.DevicePlugin_ListAndWatchServer) error {
	if err := s.Send(&dp.ListAndWatchResponse{Devices: p.devs}); err != nil {
		return err
	}
	<-p.stop
	return nil
}

// Allocate handles device allocation requests from kubelet.
func (p *RdmaDevicePlugin) Allocate(_ context.Context, req *dp.AllocateRequest) (*dp.AllocateResponse, error) {
	responses := make([]*dp.ContainerAllocateResponse, len(req.ContainerRequests))
	for i, cr := range req.ContainerRequests {
		logrus.Infof("allocating %d RDMA device(s): %v", len(cr.DevicesIDs), cr.DevicesIDs)
		responses[i] = &dp.ContainerAllocateResponse{
			Envs: map[string]string{
				"RDMA_RESOURCE":  p.resourceName,
				"RDMA_ALLOCATED": strconv.Itoa(len(cr.DevicesIDs)),
			},
		}
	}
	return &dp.AllocateResponse{ContainerResponses: responses}, nil
}

// GetDevicePluginOptions returns empty options (no pre-start needed).
func (p *RdmaDevicePlugin) GetDevicePluginOptions(_ context.Context, _ *dp.Empty) (*dp.DevicePluginOptions, error) {
	return &dp.DevicePluginOptions{}, nil
}

// PreStartContainer is a no-op.
func (p *RdmaDevicePlugin) PreStartContainer(_ context.Context, _ *dp.PreStartContainerRequest) (*dp.PreStartContainerResponse, error) {
	return &dp.PreStartContainerResponse{}, nil
}

// GetPreferredAllocation is a no-op.
func (p *RdmaDevicePlugin) GetPreferredAllocation(_ context.Context, _ *dp.PreferredAllocationRequest) (*dp.PreferredAllocationResponse, error) {
	return &dp.PreferredAllocationResponse{}, nil
}
