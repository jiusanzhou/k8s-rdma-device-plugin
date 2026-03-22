# k8s-rdma-device-plugin

Kubernetes device plugin for RDMA NIC resource management. Enables pods to request RDMA network card resources, with **automatic device permission injection** via NRI and **GPU-RDMA PCIe affinity** support.

> **TL;DR** — Deploy as a DaemonSet, GPU containers automatically get the right RDMA devices injected. No `privileged: true`, no manual device mounts, no topology guesswork.

## Why This Project?

Running distributed AI workloads (vLLM, SGLang, DeepSpeed, Megatron-LM, PyTorch DDP) on Kubernetes with RDMA networking is painful:

| Problem | Typical Workaround | This Plugin |
|---|---|---|
| Containers can't access `/dev/infiniband/*` | Run as `privileged: true` 😱 | NRI auto-injects only needed devices |
| NCCL falls back to TCP (slow) | Manually mount device nodes | Automatic — zero config |
| Wrong RDMA NIC selected (cross-NUMA) | Manually set `NCCL_IB_HCA=mlx5_X` | GPU-RDMA PCIe affinity auto-selects the closest NIC |
| Multi-tenant resource accounting | No RDMA awareness in scheduler | Device Plugin reports `rdma.io/hca` resources |

### Comparison with Existing Solutions

| Capability | [Mellanox shared-plugin](https://github.com/Mellanox/k8s-rdma-shared-dev-plugin) | [SR-IOV device-plugin](https://github.com/k8snetworkplumbingwg/sriov-network-device-plugin) | **This project** |
|---|---|---|---|
| Resource reporting to kubelet | ✅ | ✅ (VFs) | ✅ (toggleable) |
| Auto device permission injection | ❌ | ❌ | ✅ via NRI |
| GPU-RDMA PCIe topology affinity | ❌ | ❌ | ✅ |
| No `privileged: true` needed | ❌ | Partial | ✅ |
| Works without CNI changes | ✅ | ❌ (needs Multus) | ✅ |
| Single binary, lightweight | ✅ | ✅ | ✅ |

## Features

- **Device Plugin**: Reports RDMA resources to kubelet, allowing pods to request them via `resources.limits`
- **NRI Device Injection**: Automatically injects `/dev/infiniband/*` device permissions into containers via [containerd NRI](https://github.com/containerd/nri)
- **GPU-RDMA Affinity**: Automatically discovers and injects RDMA devices matching GPU PCIe topology (for NVIDIA GPU workloads)
- **Configurable**: Resource name, count, and behavior configurable via YAML file, environment variables, or CLI flags
- **Toggleable**: Device plugin resource registration can be disabled — use NRI injection only

## Architecture

```
┌──────────────────────────────────────────────────────┐
│                     kubelet                          │
│                                                      │
│  ┌─────────────────────┐  ┌───────────────────────┐  │
│  │   Device Plugin API  │  │   Pod Scheduling      │  │
│  │   (gRPC)            │  │   rdma.io/hca: 1      │  │
│  └────────┬────────────┘  └───────────────────────┘  │
│           │                                          │
└───────────┼──────────────────────────────────────────┘
            │ Register + ListAndWatch + Allocate
            │
┌───────────┴──────────────────────────────────────────┐
│         k8s-rdma-device-plugin (DaemonSet)           │
│                                                      │
│  ┌─────────────────┐  ┌────────────────────────────┐ │
│  │  Device Plugin   │  │  NRI Plugin                │ │
│  │  (gRPC Server)   │  │  (CreateContainer hook)    │ │
│  │                  │  │                            │ │
│  │  Report N virtual│  │  • Default RDMA devices    │ │
│  │  RDMA resources  │  │  • Annotation-based inject │ │
│  │  (toggleable)    │  │  • GPU-RDMA auto-inject    │ │
│  └─────────────────┘  └────────────────────────────┘ │
│                                                      │
│  ┌─────────────────────────────────────────────────┐ │
│  │  GPU-RDMA Topology Discovery                    │ │
│  │  /sys/bus/pci/drivers/nvidia → GPU BDFs         │ │
│  │  /sys/class/infiniband → RDMA devices           │ │
│  │  Match: PCIe root complex > NUMA node           │ │
│  └─────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────┘
```

## Use Cases

### 🚀 LLM Inference (vLLM / SGLang)

Multi-GPU tensor parallelism and PD disaggregation rely on NCCL over RDMA for inter-GPU communication. This plugin ensures each inference pod automatically gets the RDMA devices with correct PCIe affinity — no manual `NCCL_IB_HCA` configuration needed.

```yaml
# vLLM multi-GPU inference — RDMA devices auto-injected
apiVersion: v1
kind: Pod
spec:
  containers:
    - name: vllm
      image: vllm/vllm-openai:latest
      securityContext:
        capabilities:
          add:
            - IPC_LOCK
            - SYS_RESOURCE
            - NET_RAW
      resources:
        limits:
          nvidia.com/gpu: "4"
```

### 🏋️ Distributed Training (DeepSpeed / Megatron-LM / PyTorch DDP)

Large-scale training across nodes requires GPUDirect RDMA for efficient gradient synchronization. The plugin auto-discovers RDMA-GPU topology and injects the right devices.

### 🔗 PD Disaggregation (SGLang + Mooncake)

Prefill/Decode disaggregation uses RDMA for KV cache transfer between nodes. Instead of manually specifying `--disaggregation-ib-device mlx5_1`, the plugin automatically provides the correct RDMA devices.

### 📦 Multi-tenant GPU Clusters

Use the Device Plugin to track RDMA resource allocation across tenants, while NRI handles the actual device injection. Disable the Device Plugin (`--enable-device-plugin=false`) if you only need automatic injection without resource accounting.

## Quick Start

### Deploy with Helm

```bash
helm install rdma-device-plugin ./deploy/charts \
  --namespace kube-system \
  --set rdma.resourceName="rdma.io/hca" \
  --set rdma.resourceCount=100 \
  --set gpuRdmaAutoInject=true
```

### Use in a Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: rdma-workload
spec:
  containers:
    - name: app
      image: your-app:latest
      resources:
        limits:
          rdma.io/hca: "1"
```

The NRI plugin will automatically inject all RDMA device nodes (`/dev/infiniband/*`) into the container.

### GPU + RDMA (Automatic Affinity)

When `gpuRdmaAutoInject` is enabled, GPU containers automatically get the RDMA devices sharing PCIe affinity — **no resource request needed**:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-rdma-workload
spec:
  containers:
    - name: training
      image: nvidia/cuda:12.0-base
      securityContext:
        capabilities:
          add:
            - IPC_LOCK
            - SYS_RESOURCE
            - NET_RAW
      resources:
        limits:
          nvidia.com/gpu: "2"
```

### NRI-only Mode (No Resource Registration)

If you only need automatic device injection without kubelet resource accounting:

```bash
helm install rdma-device-plugin ./deploy/charts \
  --namespace kube-system \
  --set rdma.enableDevicePlugin=false \
  --set gpuRdmaAutoInject=true
```

## Configuration

### Config File (YAML)

```yaml
# /etc/rdma-device-plugin/config.yaml
enableDevicePlugin: true
resourceName: "rdma.io/hca"
resourceCount: 100
nriPluginName: "rdma-device-plugin"
nriPluginIndex: 10
gpuRdmaAutoInject: true
debug: false
logPath: "-"
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `RDMA_ENABLE_DEVICE_PLUGIN` | Enable device plugin resource registration (`false`/`0` to disable) | `true` |
| `RDMA_RESOURCE_NAME` | Resource name to register | `rdma.io/hca` |
| `RDMA_RESOURCE_COUNT` | Number of virtual resource slots | `100` |
| `RDMA_NRI_PLUGIN_NAME` | NRI plugin name | `rdma-device-plugin` |
| `RDMA_GPU_AUTO_INJECT` | Enable GPU-RDMA auto-injection | `false` |
| `RDMA_DEBUG` | Enable debug logging | `false` |
| `RDMA_LOG_PATH` | Log file path (`-` for stdout) | `/data/log/...` |

### CLI Flags

```
--config                 Path to YAML config file
--enable-device-plugin   Enable/disable RDMA resource registration (default: true)
--resource-name          Custom resource name
--resource-count         Number of virtual RDMA resources
--gpu-rdma-auto-inject   Enable GPU-RDMA auto-injection
--debug                  Enable debug logging
--log-path               Log file path ("-" for stdout)
```

Priority: CLI flags > Environment variables > Config file > Defaults

## How It Works

### 1. Device Plugin

The plugin registers with kubelet via the [Device Plugin Framework](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/). It reports N virtual RDMA resource slots (default: 100), allowing kubelet to schedule pods that request `rdma.io/hca` resources.

Since a single RDMA NIC doesn't support hardware-level isolation, the virtual slots are fungible — each allocation simply signals that the pod needs RDMA access. This can be toggled off with `--enable-device-plugin=false`.

### 2. NRI Device Injection

Using [containerd NRI](https://github.com/containerd/nri), the plugin hooks into the `CreateContainer` lifecycle event and injects:

- **Global devices**: `/dev/infiniband/rdma_cm`
- **Per-NIC devices**: `/dev/infiniband/uverbs*`, `/dev/infiniband/umad*`, `/dev/infiniband/issm*`

This ensures containers have the necessary device permissions without requiring `privileged: true`.

#### Required Capabilities

RDMA operations require specific Linux capabilities. Add the following `securityContext` to your container spec:

```yaml
securityContext:
  capabilities:
    add:
      - IPC_LOCK      # Lock memory for RDMA registered buffers
      - SYS_RESOURCE   # Increase memlock rlimit beyond default
      - NET_RAW        # Raw packet access for RDMA CM
```

> **Note:** These capabilities are much narrower than `privileged: true` and are the minimum required for RDMA workloads.

#### Annotation-based Injection

For fine-grained control, use annotations:

```yaml
metadata:
  annotations:
    # Inject for all containers in the pod
    devices.nri.io/pod: |
      - path: /dev/infiniband/uverbs0
        type: c
        major: 231
        minor: 0
    # Inject for a specific container
    devices.nri.io/container.myapp: |
      - path: /dev/infiniband/uverbs1
        type: c
        major: 231
        minor: 1
```

### 3. GPU-RDMA PCIe Affinity

When `gpuRdmaAutoInject` is enabled, the plugin:

1. Detects GPU containers via `NVIDIA_VISIBLE_DEVICES` environment variable
2. Enumerates GPU PCI BDFs from `/sys/bus/pci/drivers/nvidia/`
3. Enumerates RDMA devices from `/sys/class/infiniband/`
4. Matches GPUs to RDMA devices by:
   - **PCIe root complex** (highest priority — same switch fabric)
   - **NUMA node** (fallback — same memory domain)
5. Injects the matched RDMA devices into the container

This is critical for high-performance GPU-Direct RDMA workloads where network locality matters.

## Development

### Build

```bash
# Linux binary
make build

# Local (macOS/current OS)
make build-local

# Docker image
make docker IMAGE=my-registry/k8s-rdma-device-plugin TAG=latest
```

### Test

```bash
make test
```

### Lint

```bash
make lint
```

## Requirements

- **containerd ≥ 1.7** with NRI enabled (`enable_nri = true` in containerd config) — NRI (Node Resource Interface) is the mechanism used to inject RDMA devices into containers at runtime
- RDMA-capable network cards (Mellanox/NVIDIA ConnectX series)
- For GPU-RDMA affinity: NVIDIA GPU driver installed

## Contributing

Issues and PRs are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache License 2.0 — see [LICENSE](LICENSE)
