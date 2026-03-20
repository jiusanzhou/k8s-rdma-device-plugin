# k8s-rdma-device-plugin

面向 Kubernetes 的 RDMA 网卡资源管理插件。通过 NRI **自动注入设备权限**，支持 **GPU-RDMA PCIe 拓扑亲和**。

> **一句话**：部署一个 DaemonSet，GPU 容器自动获得正确的 RDMA 设备权限。不需要 `privileged: true`，不需要手动挂载设备，不需要关心拓扑。

## 为什么需要这个项目？

在 K8s 上跑分布式 AI 推理/训练（vLLM、SGLang、DeepSpeed、Megatron-LM）时，RDMA 设备管理是个坑：

| 痛点 | 常见做法 | 本插件 |
|---|---|---|
| 容器没有 `/dev/infiniband/*` 权限 | `privileged: true` 😱 | NRI 自动注入所需设备 |
| NCCL 退化到 TCP（性能暴跌） | 手动挂载设备节点 | 自动注入，零配置 |
| 选错 RDMA 卡（跨 NUMA） | 手动设 `NCCL_IB_HCA=mlx5_X` | 根据 PCIe 拓扑自动选最近的卡 |
| 多租户无 RDMA 资源管控 | 无感知 | Device Plugin 上报 `rdma.io/hca` 资源 |

### 与现有方案对比

| 能力 | [Mellanox shared-plugin](https://github.com/Mellanox/k8s-rdma-shared-dev-plugin) | [SR-IOV device-plugin](https://github.com/k8snetworkplumbingwg/sriov-network-device-plugin) | **本项目** |
|---|---|---|---|
| 资源上报到 kubelet | ✅ | ✅ (VF) | ✅（可关闭） |
| 自动注入设备权限 | ❌ | ❌ | ✅（NRI） |
| GPU-RDMA PCIe 拓扑亲和 | ❌ | ❌ | ✅ |
| 无需 privileged | ❌ | 部分 | ✅ |
| 无需改 CNI | ✅ | ❌（依赖 Multus） | ✅ |

## 典型场景

- **LLM 推理**（vLLM / SGLang）：多卡 TP 和 PD 分离依赖 NCCL over RDMA
- **分布式训练**（DeepSpeed / Megatron-LM）：GPUDirect RDMA 加速梯度同步
- **PD 分离**（SGLang + Mooncake）：RDMA 传输 KV cache，无需手动指定 `--disaggregation-ib-device`
- **多租户 GPU 集群**：跟踪 RDMA 资源分配

详细文档见 [README.md](README.md)（英文）。

## 快速开始

```bash
helm install rdma-device-plugin ./deploy/charts \
  --namespace kube-system \
  --set gpuRdmaAutoInject=true
```

## License

Apache License 2.0
