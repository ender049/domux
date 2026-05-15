# domux

`domux` 是一个面向个人自托管用户的多节点应用入口管理器，用于统一管理 Docker / Podman 应用、子域名入口、DDNS 和证书。

适合：homelab、自托管、多台小规模主机、DDNS 与通配符证书场景。

不适合：通用边缘平台、完整 ingress controller、面向大团队的反向代理平台。

## 核心特性

- **Docker 和 Podman 自动发现**，支持本地或远程运行时
- **基于 `domux.*` 容器标签的子域名自动路由**
- **自定义应用代理**，可手动为现有域名增加指向任意上游站点的访问入口
- **自定义应用代理节点选择**，可让外部站点经指定 Agent 节点代理访问
- **HTTP / HTTPS 代理能力**，同时支撑本地入口和按节点选择的应用出口
- **DDNS**，支持可配置的公网 IP 探测端点
- **ACME DNS-01 证书管理**，与 DDNS 共享同一套 provider 配置
- **证书部署**，支持本地写入、SSH 和远程 agent
- **控制台和 API 管理**
- **以 `config.yaml` 为唯一事实来源**，受管对象修改后可在进程内重载

## 当前能力

- 本地 Docker 和 Podman 自动发现
- 通过 agent 实现远程 Docker 和 Podman 自动发现
- DDNS provider 管理与同步
- ACME DNS-01 证书签发与续期
- 向本地、SSH 和 agent 目标部署证书
- 基于 `config.yaml` 的控制台管理
- 通过控制台推荐脚本安装 agent，或手工执行 `domux-agent install ...`；两者都会走主动注册和审批流程

## 推荐使用方式

### Docker 主机

如果你的主机使用 Docker，并且 `domux` 可以访问 Docker daemon，推荐优先采用这条路径：

- 用容器方式运行 `domux`
- 挂载 `/var/run/docker.sock`
- 在 `config.yaml` 中使用 `runtime: docker` 和 `endpoint: unix:///var/run/docker.sock`
- 默认先使用高位端口映射，例如 `18080/8080/8443`

在这种部署方式下，如果 `domux` 与被发现工作负载之间的 Docker bridge 网络地址可达，通常可以直接通过容器网络地址访问上游。

### Rootless Podman 主机

如果你的主机使用 rootless Podman，推荐按下面方式部署：

- 用容器方式运行 `domux`
- 挂载用户 Podman socket，例如 `/run/user/<uid>/podman/podman.sock:/run/podman/podman.sock`
- 在 `config.yaml` 中使用 `runtime: podman` 和 `endpoint: unix:///run/podman/podman.sock`
- 确保 `domux` 容器内可以访问 `host.containers.internal`，或等效的宿主机别名
- 默认使用高位端口映射，例如 `18080/8080/8443`

在 rootless Podman 场景下，如果工作负载的 bridge IP 不能被 `domux` 直接访问，应为工作负载发布端口；`domux` 会通过 `host.containers.internal:<publishedPort>` 一类的宿主机可达地址访问上游。

## 快速开始

### 1. 创建配置文件

复制示例配置，并按你的 domain 和 DNS provider 进行修改：

```bash
cp config.example.yaml config.yaml
```

通常至少需要修改：

- `ddns_providers`
- `domains`
- `server.runtime`

`server.data_dir` 是运行时目录，用于保存 ACME 账户、签发证书和其他本地状态。它不应作为仓库内容提交，默认也已被 `.gitignore` 忽略。

常见配置项：

- `server`：API、HTTP、HTTPS 监听地址和可选 Basic Auth
- `server.public_ip`：公网 IPv4/IPv6 探测端点
- `ddns_providers`：DNS provider 凭据定义
- `apps`：手动添加的自定义应用
- `server.runtime`：控制节点本地 Docker / Podman 运行时
- `agents`：已审批的远程节点
- `deploy_targets`：证书部署目标（`local` / `agent` / `ssh`）
- `domains`：域名空间、DDNS 和证书配置

当前正式支持的 provider 只有 4 个，且同一份 provider options 会同时用于 DDNS 和 DNS-01：

- `cloudflare`: `api_token`
- `alidns`: `access_key_id` + `access_key_secret`
- `godaddy`: `api_key` + `api_secret`
- `spaceship`: `api_key` + `api_secret`

其中 Cloudflare 按当前官方推荐使用 API Token。

### Cloudflare Token 使用建议

对 Cloudflare，推荐优先按“**管理目标域名下的 DNS 记录**”来准备 token。

#### 推荐默认

适用于 DDNS、DNS-01、以及在已有域名下创建 `*.example.com` 入口。

- 建议权限：`DNS Read` + `DNS Write`
- 如需让产品读取域名信息，可再加：`Zone Read`
- 资源范围：可限制到你实际要管理的域名，例如 `vlrat.com`

这类权限可以覆盖：

- 读取域名状态
- 读写该域名下的普通子域名记录
- 支撑 DDNS / DNS-01 / 子域名入口管理

大多数 `domux` 用户不需要额外准备更高的 Cloudflare 域名管理权限；只要能读写目标域名下的 DNS 记录，通常就足以完成主使用场景。

### 2. 给要暴露的工作负载打标签

下面是一个 Docker Compose 服务标签示例：

```yaml
services:
  whoami:
    image: traefik/whoami
    ports:
      - "8081:80"
    labels:
      domux.domain: "home.example.com"
      domux.subdomain: "whoami"
      domux.port: "80"
```

只有其他有效 `domux.*` 标签才表示该工作负载需要纳管。

### 3. 启动 domux 服务端

```bash
go run ./cmd/domux -config ./config.yaml
```

然后在浏览器中打开：

```text
http://127.0.0.1:18080/
```

如果你把 `domux` 运行在 rootless Podman 容器里，推荐先映射高位端口，例如宿主机 `8080->容器 8080`、`8443->容器 8443`。本机默认 `net.ipv4.ip_unprivileged_port_start=1024` 时，rootless Podman 不能直接绑定宿主机 `80/443`；只有在你额外完成宿主机侧低端口配置后，才适合暴露 `80/443`。

### 4. 按需接入远程 agent

正式安装方式有两种：控制台推荐脚本安装，或手工执行 `domux-agent install ...`。

在控制台中：

1. 打开 **节点** 页面
2. 点击 **安装命令**
3. 将生成的命令复制到目标主机执行；如果保留默认安装前缀 `/etc/domux`，通常需要配合 `sudo`
4. 回到节点页，审批新注册节点

默认命令只需要控制台地址：

```bash
domux-agent install -server 'http://127.0.0.1:18080'
```

agent 启动后会主动注册，控制台审批通过后再写入 `config.yaml`。

### 5. 证书部署目标

`deploy_targets` 支持三种传输方式：

- `local`：写入当前主机本地路径
- `agent`：通过已审批 agent 写入远程节点
- `ssh`：通过 SSH 上传到远程主机

使用 `ssh` 目标时，请预先创建目标目录；当前不会自动创建远端证书目录。

## 运维说明

- `config.yaml` 是唯一配置来源
- 控制台/API 的修改会回写到 `config.yaml`，并在进程内重载
- 直接手工修改 `config.yaml` 仍然是权威配置，但目前**不会自动监听文件变化**；手动编辑后需要重启 domux 服务
- `data_dir` 是运行时数据目录，应使用持久化路径，并避免提交到版本库
- `server.auth`、`server.api_addr`、`server.http_addr` 和 `server.https_addr` 属于启动级配置
- 容器化 rootless Podman 部署时，推荐把本地运行时配置成 `runtime: podman` + `endpoint: unix:///run/podman/podman.sock`，并挂载对应 Podman socket 到容器内
- 当 `domux` 运行在容器内并发现 Podman 已发布端口的工作负载时，上游访问会走 `host.containers.internal:<publishedPort>`（或容器内可达的等效宿主机别名），不是容器回环地址
- 本地 rootless Podman 场景下，如果 bridge 网络中的容器 IP 无法被 domux 进程直接访问，应为工作负载发布端口
- 证书和 ACME 账户资料保存在 `data_dir`

## 致谢

`domux` 在演进过程中参考过一些优秀项目的思路与实现方式，尤其包括：

- `godoxy`
- `ddns-go`
- `lego`

感谢这些项目及其维护者提供的思路、实现经验和生态基础。
