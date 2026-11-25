# Flashcat Huatuo 回归测试清单

本清单用于锁定 Flashcat Huatuo 交付版本（尤其是 Categraf DaemonSet 内置 Huatuo 持续剖析）的核心验收与回归项，供测试团队在发版前独立复核，并在后续缺陷修复与新特性迭代中持续追加。

## 维护约定

- **用例 ID 稳定**：每个用例使用固定 ID（`CP-K8S-*` / `CP-DS-*` / `CP-E2E-*`），只增不重排，便于缺陷单与发版验收单引用。
- **缺陷必入清单**：凡是在真实环境或 E2E 中复现并修复的故障（例如宿主机软链逃逸、Docker 私有 cgroup v2 namespace、token 重定向泄漏），合并 PR 时同步补入一条可复核用例。
- **验收记录留痕**：每次发布 `v2.3.0-flashcat.1-*` 交付 tag 前，测试按本清单逐项复核，并在文末「发版验收记录」追加一行结论。

---

## 一、K8s / K3s × 容器运行时 4 组合矩阵

> **统一前置条件**：
> - Huatuo 运行在特权 DaemonSet 容器内（`privileged: true`、`hostPID: true`、`hostNetwork: true`），宿主机根挂载在 `/hostfs` 且环境变量 `HOST_MOUNT_PREFIX=/hostfs`；**不要求**额外部署侧配置 `DOCKER_HOST`。
> - 节点内核 ≥ 4.18（建议 cgroup v2 节点，如 Debian 12 / Ubuntu 22.04+ / RHEL 9），Doris 已联通且建好 `huatuo_bamai` 库。
> - 目标业务 Pod 运行持续消耗 CPU 的容器（如含已知热点函数 `main.burnCPU` / `main.fib` 的 Go 测试负载）。

| 用例 ID | 集群发行版 | 容器运行时 | 重点验证路径 | 操作步骤 | 通过标准 | 引入 PR |
|---|---|---|---|---|---|---|
| `CP-K8S-01` | 标准原生 K8s（kubeadm） | containerd | 原生 kubelet（`/var/lib/kubelet/config.yaml`）+ `/run/containerd/containerd.sock` + `HOST_MOUNT_PREFIX` 挂载解析 | 1. 在 DaemonSet 节点上向 Huatuo `/tasks` 提交 `-l go --container-id <cid>` CPU 采样（≥ 20s）；2. 轮询 `/tasks/<id>` 至终态；3. 查询 Doris `profiling_metadata` 对应 `tracer_id`。 | 任务状态 `completed`；全部采样窗口成功落盘 Doris；火焰图/栈数据包含目标业务热点符号且无空样本。 | #11 |
| `CP-K8S-02` | 标准原生 K8s（kubeadm） | Docker（`cri-dockerd`） | `/var/run -> /run` 绝对软链在 `HOST_MOUNT_PREFIX` 下重锚定；无 `DOCKER_HOST` 自动发现 `/hostfs/run/docker.sock`；Docker 20.10+ 私有 cgroup v2 namespace 切换（`setns` 到 PID 1 cgroupns + `/hostfs/sys/fs/cgroup`） | 同 `CP-K8S-01`，确认 DaemonSet 容器未设置 `DOCKER_HOST` 环境变量，且容器处于非根 cgroup v2 namespace（`/proc/self/ns/cgroup != /proc/1/ns/cgroup`）。 | 任务状态 `completed`；`container_provider` 正常连接宿主机 Docker socket；Doris 落盘非空且包含目标业务热点符号。 | #11 |
| `CP-K8S-03` | K3s | containerd（内置） | K3s 非标准路径 `/run/k3s/containerd/containerd.sock` 与 `/run/k3s/containerd` 状态目录在 `HOST_MOUNT_PREFIX` 下解析 | 同 `CP-K8S-01`。 | 任务状态 `completed`；全部窗口落盘 Doris 且包含目标业务热点符号。 | #11 |
| `CP-K8S-04` | K3s | Docker（`k3s --docker`） | K3s 场景下无 `DOCKER_HOST` 发现宿主机 Docker socket + 私有 cgroup v2 namespace 解析 | 同 `CP-K8S-02`。 | 任务状态 `completed`；全部窗口落盘 Doris 且包含目标业务热点符号。 | #11 |

---

## 二、DaemonSet 凭据、权限与边界安全回归

| 用例 ID | 场景 | 前置条件 / 构造方法 | 预期结果 | 验证方式 | 引入 PR |
|---|---|---|---|---|---|
| `CP-DS-01` | kubelet ServiceAccount token 认证与轮转 | kubelet 关闭只读端口（`10255`），开启授权端口（`10250`）；DaemonSet 不配客户端证书，仅挂载 Pod ServiceAccount token；模拟更新 token 文件内容。 | 正常通过 `https://127.0.0.1:10250/pods` 拉取 Pod 列表；token 文件更新后无需重启 Huatuo，后续请求自动携带新 token。 | 单测 `TestKubeletTokenClientReReadsRotatedToken` + K8s 实测 | #11 |
| `CP-DS-02` | 仅配只读端口失败时不误打 `https://127.0.0.1:0` | 配置 `PodReadOnlyPort != 0`、`PodAuthorizedPort == 0`，且 Pod 内存在 ServiceAccount token；模拟只读端口不可达。 | 直接返回只读 HTTP 端口原始错误，不触发 HTTPS 请求，报错中不含 `https://`。 | 单测 `TestReadOnlyPortFailureWithoutAuthorizedPortSkipsHTTPS` | #11 |
| `CP-DS-03` | kubelet 重定向不泄漏 ServiceAccount token | 构造模拟 kubelet HTTPS 端点返回 `302 Found` 跳转到另一 HTTP 或 HTTPS 地址。 | 客户端禁止跟随重定向，直接按 `status: 302` 报错返回；目标地址收到 0 次请求，不泄漏 `Authorization: Bearer` 头。 | 单测 `TestKubeletTokenClientDoesNotFollowRedirects` | #11 |
| `CP-DS-04` | 空本地状态目录与空 tracefs 回退 | 容器内存在空的 `/run/containerd` 或 `/var/lib/docker` 目录，真实数据在 `/hostfs/...`；容器内 `/sys/kernel/tracing` 为空，宿主机在 `/hostfs/sys/kernel/tracing`。 | 运行时状态目录跳过空本地目录并命中 `/hostfs/...`；kprobe/raw_tracepoint 探测走 `HOST_MOUNT_PREFIX` 成功，普通 tracepoint（RAS）按本地缺失平滑跳过而非 attach 崩溃。 | 单测 `TestContainerInitPID*`、`TestHasKprobeFunctionReadsHostCopyOnlyWithoutLocal`、`TestTracepointAvailableUnderHostMountPrefix` | #11 |
| `CP-DS-05` | 私有 cgroupns 读宿主失败时不折叠为根 cgroup `/` | 私有 cgroupns 下 `/proc/<pid>/cgroup` 返回 `0::/../..` 或 `0::/../../kubepods.slice/...`，或 `setns` 无权限失败。 | `PathsForPID` / `parseProcessPaths` 明确返回错误，绝不把逃逸路径静默清洗为 `/`（避免误绑定宿主机根 cgroup）。 | 单测 `TestParseProcessPaths/escaped_namespace_*` | #11 |

---

## 三、平台端到端验收（Flashcat 9000 → Categraf → Huatuo → Doris → UI）

| 用例 ID | 验收项 | 操作步骤 | 通过标准 | 引入 PR |
|---|---|---|---|---|
| `CP-E2E-01` | Preflight 最低版本与资产校验门禁 | 1. 节点运行旧版 Categraf（`< ent-v0.5.52` 或带 pre-release 后缀），在 9000 创建 K8s 剖析任务触发 preflight；2. 升级为 `>= ent-v0.5.52` 且内置匹配的 `bundled/huatuo.json` + Huatuo 二进制后重跑 preflight。 | 旧版本在 `agent_version` 阶段被明确拦截并提示 `minimum_version: ent-v0.5.52`（Host 剖析仍为 `ent-v0.5.51`）；升级后 `target` / `agent_version` / `kernel_version` / `huatuo` / `doris` 5 阶段全部 `pass`。 | #11 / n9e-plus#1549 |
| `CP-E2E-02` | 多节点 DaemonSet 全链路多指标采集 | 在 9000 对 3 节点 Go DaemonSet（如 `go-ds`）创建单次 20s 剖析任务，勾选 `cpu` + `offcpu:blocked` + `memory:physical_alloc`。 | 任务终态 `completed`，3/3 节点均 `status=success`、`empty_types=[]`；`/api/n9e-plus/continuous-profiling/flamegraph` 对 3 类指标均返回非空火焰图；前端详情页记录表与底部火焰图抽屉正常渲染、无报错请求。 | #11 / n9e-plus-fe#1306 |
| `CP-E2E-03` | 多语言与多工作负载覆盖 | 在 K8s 集群分别对 C / C++ / Go / Java / Python 的 Deployment 与 DaemonSet 目标执行单次剖析（Native：`cpu`/`offcpu`/`memory`；Java：`cpu`/`memory:object_alloc`/`memory:object_usage`；Python：`cpu`）。 | 各类语言与工作负载均能成功完成采样，火焰图符号可读（非全 `[unknown]`）。 | #11 |
| `CP-E2E-04` | 持续剖析停止与清理 | 创建 `mode=continuous` 持续剖析任务，跑完 ≥ 2 轮后执行暂停/删除，登录目标节点检查进程与临时文件。 | 节点无残留 `profiler` / `asprof` / `py-spy` 孤儿进程；无失控临时文件堆积。 | #11 |

---

## 四、发版验收记录

| 版本 / Commit | 日期 | 执行环境 | 覆盖用例 | 结论 | 备注 |
|---|---|---|---|---|---|
| PR #11 (`fad8f597` / `304af735`) | 2026-11-25 | `fncloud` KVM 4 组合矩阵 + `10.98.1.106:9000` → `fn-k8s` 3 节点 K3s | `CP-K8S-01`~`04`, `CP-DS-01`~`05`, `CP-E2E-01`~`02` | 通过 | 4 组合矩阵全部采集到 `main.burnCPU`；9000 3 节点 `go-ds` 3 指标全绿且 UI 火焰图渲染正常 |
