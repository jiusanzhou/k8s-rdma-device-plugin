# Reddit Posts for k8s-rdma-device-plugin

---

## Post 1: r/kubernetes

**URL**: https://www.reddit.com/r/kubernetes/submit?type=TEXT

**Title**: Auto RDMA device injection for GPU containers — no privileged mode needed (open-source)

**Body**:

I've been dealing with RDMA device management pain when running distributed AI workloads (vLLM, SGLang, DeepSpeed) on K8s clusters with InfiniBand/RoCE. The common issues:

- Containers can't access `/dev/infiniband/*` → NCCL silently falls back to TCP (massive perf drop)
- Everyone uses `privileged: true` as a workaround 😬
- Multi-NIC nodes: NCCL picks the wrong RDMA card (cross-NUMA), have to manually set `NCCL_IB_HCA`

Built an open-source solution: **[k8s-rdma-device-plugin](https://github.com/jiusanzhou/k8s-rdma-device-plugin)**

**What it does:**

1. **NRI-based device injection** — uses containerd NRI to auto-inject `/dev/infiniband/*` devices into containers at creation time. No privileged mode, no hostPath mounts.

2. **GPU-RDMA PCIe affinity** — discovers which RDMA NIC is physically closest to each GPU via sysfs PCIe topology, and injects only the right devices. Critical for GPUDirect RDMA performance.

3. **Optional device plugin** — reports `rdma.io/hca` resources to kubelet for scheduling (can be disabled if you only want injection).

**How it compares:**

| | Mellanox shared-plugin | SR-IOV plugin | This project |
|---|---|---|---|
| Resource reporting | ✅ | ✅ | ✅ (toggleable) |
| Auto device injection | ❌ | ❌ | ✅ via NRI |
| GPU-RDMA PCIe affinity | ❌ | ❌ | ✅ |
| No privileged needed | ❌ | Partial | ✅ |

Deploy as a DaemonSet with `gpuRdmaAutoInject=true`, and any pod with `NVIDIA_VISIBLE_DEVICES` automatically gets the correct RDMA devices. Zero config needed in your workload manifests.

Written in Go, ~1300 LOC, Apache 2.0 licensed. Feedback welcome!

---

## Post 2: r/LocalLLaMA

**URL**: https://www.reddit.com/r/LocalLLaMA/submit?type=TEXT

**Title**: Open-source K8s plugin that auto-injects RDMA devices into GPU containers (makes vLLM/SGLang multi-node inference just work)

**Body**:

If you're running vLLM or SGLang with tensor parallelism across multiple GPUs on Kubernetes with InfiniBand/RoCE networking, you've probably hit this: NCCL can't find the RDMA devices because your container doesn't have `/dev/infiniband/*` access, and silently falls back to TCP.

The usual fix is `privileged: true` (bad) or manually mounting device nodes (fragile). On multi-NIC nodes you also need to figure out which `mlx5_X` device maps to which GPU for optimal topology.

I open-sourced **[k8s-rdma-device-plugin](https://github.com/jiusanzhou/k8s-rdma-device-plugin)** to handle this automatically:

- **Auto-injects RDMA device permissions** into GPU containers via containerd NRI — no privileged mode
- **GPU-RDMA PCIe affinity** — figures out which RDMA NIC is closest to each GPU and injects only those devices
- Works with any framework that uses NCCL (vLLM, SGLang, DeepSpeed, Megatron-LM, PyTorch DDP)

For SGLang PD disaggregation users: you no longer need to manually specify `--disaggregation-ib-device mlx5_1` — the right device is already in the container.

```bash
helm install rdma-device-plugin ./deploy/charts \
  --namespace kube-system \
  --set gpuRdmaAutoInject=true
```

That's it. GPU pods automatically get RDMA access with correct topology.

GitHub: https://github.com/jiusanzhou/k8s-rdma-device-plugin

Would love feedback from anyone running multi-GPU inference on K8s with RDMA!
