# vLLM Discussion Post

## URL
https://github.com/vllm-project/vllm/discussions/new?category=show-and-tell

## Title
Auto RDMA device injection for GPU containers on Kubernetes (no privileged mode needed)

## Body

Hey vLLM community! 👋

Running vLLM with tensor parallelism or pipeline parallelism on Kubernetes clusters with InfiniBand/RoCE networking often hits RDMA device management issues:

- Containers need `/dev/infiniband/*` devices for NCCL to use RDMA, but don't have them by default
- Common fix is `privileged: true` or manually mounting device nodes — fragile and insecure
- On multi-NIC nodes, NCCL may pick the wrong RDMA card (cross-NUMA), causing significant performance degradation
- Users have to manually set `NCCL_IB_HCA=mlx5_X` to select the right NIC

I built **[k8s-rdma-device-plugin](https://github.com/jiusanzhou/k8s-rdma-device-plugin)** to solve this. It's a Kubernetes DaemonSet that:

1. **Auto-injects RDMA device permissions** into containers via [containerd NRI](https://github.com/containerd/nri) — no `privileged: true` needed
2. **GPU-RDMA PCIe affinity** — discovers which RDMA NIC is closest to each GPU (PCIe root complex / NUMA node matching) and injects only the right devices
3. **Optional resource accounting** — reports `rdma.io/hca` resources to kubelet for scheduling

### Comparison with existing solutions

| Capability | Mellanox shared-plugin | SR-IOV plugin | **This project** |
|---|---|---|---|
| Resource reporting | ✅ | ✅ | ✅ (toggleable) |
| Auto device permission injection | ❌ | ❌ | ✅ via NRI |
| GPU-RDMA PCIe topology affinity | ❌ | ❌ | ✅ |
| No `privileged: true` needed | ❌ | Partial | ✅ |
| Works without CNI changes | ✅ | ❌ | ✅ |

### How it helps vLLM deployments

With `gpuRdmaAutoInject=true`, any pod with `NVIDIA_VISIBLE_DEVICES` automatically gets the correct RDMA devices:

```yaml
apiVersion: v1
kind: Pod
spec:
  containers:
    - name: vllm
      image: vllm/vllm-openai:latest
      args: ["--model", "deepseek-ai/DeepSeek-V3", "--tensor-parallel-size", "8"]
      env:
        - name: NVIDIA_VISIBLE_DEVICES
          value: "0,1,2,3,4,5,6,7"
      resources:
        limits:
          nvidia.com/gpu: "8"
  # RDMA devices auto-injected with correct PCIe affinity!
```

This is especially relevant for the large-scale serving roadmap (GB200, Wide EP, P/D disaggregation) where RDMA performance is critical.

### Quick deploy

```bash
helm install rdma-device-plugin ./deploy/charts \
  --namespace kube-system \
  --set gpuRdmaAutoInject=true
```

Repo: https://github.com/jiusanzhou/k8s-rdma-device-plugin

Feedback welcome from anyone running vLLM on RDMA-capable K8s clusters!
