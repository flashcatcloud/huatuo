# Flashcat Huatuo 制品发布

流程：固定 tag → 原生 amd64/arm64 构建 → GNU tar 归档 → 解包检查和 CLI 启动 → 两架构成功后上传 COS → 下载回读 SHA256 校验。

在 GitHub `release` environment 配置 `FC_COS_SECRETID`、`FC_COS_SECRETKEY`、`FC_COS_BUCKET` secrets；可选 `FC_COS_REGION` variable，默认 `ap-nanjing`。凭据需要对应 `huatuo/` 前缀的读写权限。采用 GitHub hosted Ubuntu 24.04 x64/ARM64 runner，需要仓库账号支持并有 Actions 额度。

推送版本 tag 或手动触发 `Flashcat Huatuo artifacts` 并填写已存在的 tag。手动触发也必须指向真实 tag，不接受普通分支。正式触发前必须先提交本流程；旧 tag 中没有这些脚本时不能用于首次构建。

对象路径：`huatuo/<tag>/huatuo-bamai-<tag>-doris-static-linux-<arch>.tar.gz`，旁边有 `.sha256` 文件。沿用 Categraf 下载域名时，消费者 URL 为 `https://download.flashcat.cloud/<对象路径>`；域名访问规则与 Referer 要求需以实际 CDN 配置为准。上传使用 COS SDK 签名访问，不依赖 CDN。

同名对象禁止覆盖；重跑只接受远端字节完全相同的对象。构建固定源码、采样器下载校验和及归档时间，不承诺不同工具链下二进制逐字节相同；内容变化必须发布新版本。

包内包含 `bin/`、`bpf/`、`conf/`、LICENSE、build.json；采样器复用 `build/install-tools.sh` 的固定版本和哈希，保留原始 `tools/`，同时按 Categraf 契约提供 `bin/asprof`、`lib/libasyncProfiler.so`、根目录 `py-spy`。

当前验证涵盖 ELF 架构、华佗静态链接、BPF 对象、解包内容一致性及 CLI 启动。真实内核 BPF 挂载、Doris 写入、采样停止清理与 Categraf 准入需要另做运行验收；制品发布成功不自动放开准入。

原上游 Docker/GitHub Release workflow 保留，Flashcat 仓库禁用其执行以避免同一个 tag 发布到上游镜像名。
