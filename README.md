# DynaStub – Kubernetes 动态行为注入框架

## 项目概述

DynaStub 是一个基于 Kubernetes 的动态行为注入框架，通过声明式配置实现对容器内可执行文件的行为修改，支持运行期动态更新规则，无需重启 Pod。

### 核心能力

在 Pod 内不重启容器的前提下，**动态拦截、修改、扩展容器内任何可执行文件**（包括系统命令、脚本、甚至某些库调用）的行为，并支持规则的热更新。

### 基于此核心能力，可衍生出以下应用场景：

- **安全**：命令审计、拦截、沙箱
- **测试**：故障注入、录制回放、资源模拟
- **运维**：动态配置、日志轮转、监控注入
- **兼容性**：命令适配、环境变量覆盖
- **网络**：DNS 劫持、流量镜像

## 架构设计

```
┌─────────────────────────────────────────────────────────────┐
│                    DynaStub Operator                       │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐  │
│  │  Controller  │  │   Webhook    │  │  用户脚本目录     │  │
│  │  (Watch CR)  │  │ (Pod注入)    │  │  (hostPath)      │  │
│  └──────┬───────┘  └──────┬───────┘  └────────┬─────────┘  │
│         │                 │                    │            │
│         ▼                 ▼                    ▼            │
│  ┌──────────────────────────────────────────────────────┐  │
│  │                    Sidecar Container                  │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌────────────┐  │  │
│  │  │ CR Watch     │  │ Script Copy  │  │ emptyDir   │  │  │
│  │  │ (监听CR)     │  │ (复制脚本)   │  │ Volume     │  │  │
│  │  └──────────────┘  └──────────────┘  └─────┬──────┘  │  │
│  └─────────────────────────────────────────────┼─────────┘  │
│                                                │            │
│  ┌─────────────────────────────────────────────▼─────────┐  │
│  │  Target Pod (业务容器)                                 │  │
│  │  ┌──────────────┐  ┌──────────────┐                  │  │
│  │  │ 可执行文件    │  │ 其他原生命令 │                  │  │
│  │  │ (subPath挂载)│  │ (未注入)     │                  │  │
│  │  │ → 用户脚本   │  │              │                  │  │
│  │  └──────────────┘  └──────────────┘                  │  │
│  └─────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### 核心组件

1. **Controller**：监听 CR 变化，管理 Webhook 配置
2. **Webhook**：拦截 Pod 创建请求，注入 Sidecar 容器和相关配置
3. **Sidecar**：监听 CR 变化，将用户脚本复制到 emptyDir Volume
4. **用户脚本目录**：用户编写的包装脚本，通过 hostPath 挂载到 Sidecar

## 技术架构

DynaStub 设计并实现了基于 Operator + Mutating Webhook + Sidecar 的无重启命令拦截架构，支持秒级热更新。

### 关键技术

- **K8s Operator**：基于 Controller-Runtime 框架，实现声明式资源管理
- **Mutating Webhook**：拦截 Pod 创建请求，自动注入 Sidecar 容器
- **原生 Sidecar**：利用 Kubernetes 1.28+ 原生 Sidecar 特性，确保初始化顺序
- **emptyDir + subPath 动态挂载**：实现脚本的实时更新和命令拦截
- **用户脚本直接挂载**：用户编写包装脚本，Sidecar 直接复制到目标位置

## 功能特性

- **用户编写脚本**：用户直接编写包装脚本，无需学习复杂规则语法
- **CR 驱动**：通过自定义资源（CR）声明式定义注入规则
- **动态热更新**：脚本变更秒级生效，无需重启 Pod
- **双模式支持**：Local 模式（注入到目标 Pod）和 Remote 模式（独立 FakePod）
- **最大灵活性**：用户可编写任意复杂逻辑的脚本
- **简单易用**：只需编写脚本文件，无需编译或复杂配置
- **云原生**：完整的 Helm Chart 支持，基于 Kubernetes Operator 模式

## 核心应用场景

### 1. 安全：命令审计与拦截
- **场景**：容器内执行高危命令时的实时审计、告警或直接拦截
- **实现**：用户编写包装脚本记录命令执行，Webhook 注入 Sidecar 挂载关键命令
- **价值**：零信任容器安全防线，适合多租户集群的安全合规场景

### 2. 测试：故障注入与混沌工程
- **场景**：测试应用对文件系统错误、网络超时、磁盘满等异常的容错能力
- **实现**：用户编写脚本注入人为延迟、修改返回内容、随机返回错误
- **价值**：无侵入、秒级生效的系统调用级故障模拟

### 3. 运维：动态配置与监控注入
- **场景**：为业务容器注入日志轮转、监控探针、流量代理等运维工具
- **实现**：Webhook 根据标签自动注入 Sidecar，共享 Volume 实现日志轮转和监控数据收集
- **价值**：实现声明式运维注入，集群管理员只需定义规则

### 4. 兼容性：多版本命令适配
- **场景**：解决业务代码与基础镜像中命令版本不兼容问题
- **实现**：用户编写包装脚本解析并转换参数，适配不同版本命令
- **价值**：解决"基础镜像版本锁死"与"业务新特性需要新命令"之间的矛盾

### 5. 网络：DNS 劫持与流量镜像
- **场景**：临时修改容器内域名解析，指向特定 IP 或镜像网络流量
- **实现**：用户编写脚本覆盖网络相关命令，实现 DNS 解析劫持
- **价值**：无重启的 DNS 劫持，适合多环境联调、A/B 测试、服务迁移验证

## 快速开始

### 前置条件

- Kubernetes 集群 (>= 1.20)
- kubectl 已配置
- Helm 3

### 部署 Operator

```bash
# 安装 Operator（CRD 会自动创建）
helm install dynastub ./charts/dynastub

# 查看部署状态
kubectl get pods -l app.kubernetes.io/name=dynastub
```

### 创建第一个 BehaviorStub

```yaml
apiVersion: dynastub.example.com/v1
kind: BehaviorStub
metadata:
  name: example-stub
  namespace: default
spec:
  mode: local
  localConfig:
    targetSelector:
      matchLabels:
        app: myapp
    sidecarImage: dynastub-sidecar:latest
  scriptVolume:
    hostPath: /home/scripts/dynastub
    mountPath: /data/scripts
  behaviors:
    - name: docker
      targetPath: /usr/bin/docker
      scriptPath: /data/scripts/docker-wrapper.sh
      enableLogging: true
      logPath: /tmp/dynastub-docker.log
```

```bash
kubectl apply -f behavior-stub.yaml
```

### 为目标 Pod 添加注入标签

```bash
kubectl label pod <pod-name> dynastub-inject=enabled
```

或者创建 Pod 时添加标签：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: myapp
  labels:
    app: myapp
    dynastub-inject: enabled
spec:
  containers:
    - name: myapp
      image: myapp:latest
```

## 配置详解

### Behavior 配置字段

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 行为名称，用于标识 |
| `targetPath` | string | 是 | 目标可执行文件路径 |
| `scriptPath` | string | 是 | 用户脚本路径（相对于 scriptVolume.mountPath） |
| `enableLogging` | bool | 否 | 是否启用日志记录（默认：false） |
| `logPath` | string | 否 | 日志文件路径（默认：/tmp/dynastub-{name}.log） |

**注意**：由于使用 subPath 挂载会覆盖原命令，且业务容器文件系统通常只读，无法自动备份原命令。如需在脚本中调用原命令，请在构建镜像时预先将原命令备份为 `.original` 版本。

### 用户脚本编写指南

用户需要编写包装脚本，脚本应该：

1. **处理参数**：接收所有命令行参数
2. **实现逻辑**：根据参数执行自定义逻辑
3. **透传处理**：未处理的命令透传给原命令
4. **日志记录**：可选记录命令执行信息

### 完整配置示例

```yaml
apiVersion: dynastub.example.com/v1
kind: BehaviorStub
metadata:
  name: example-stub
  namespace: admin
spec:
  mode: local
  localConfig:
    targetSelector:
      matchLabels:
        app: myapp
        dynastub-inject: enabled
    sidecarImage: dynastub-sidecar:latest
    sidecarResources:
      requests:
        cpu: 50m
        memory: 32Mi
      limits:
        cpu: 200m
        memory: 128Mi
  scriptVolume:
    hostPath: /home/scripts/dynastub
    mountPath: /data/scripts
  behaviors:
    - name: docker
      targetPath: /usr/bin/docker
      scriptPath: /data/scripts/docker-wrapper.sh
      enableLogging: true
      logPath: /tmp/dynastub-docker.log

    - name: chown
      targetPath: /usr/bin/chown
      scriptPath: /data/scripts/chown-wrapper.sh
      enableLogging: true

  advanced:
    enableLogging: true
    logPath: /tmp/dynastub.log
    syncIntervalSeconds: 5
```

## 工作原理

### 1. 用户脚本准备

用户编写包装脚本，放置在指定的 hostPath 目录中。脚本需要处理命令行参数并实现自定义逻辑。

### 2. 命令拦截机制

使用 Kubernetes 的 `emptyDir` Volume + `subPath` 挂载实现命令遮盖，确保目标可执行文件被用户脚本替换。

### 3. 动态更新流程

```
用户修改脚本文件 (/home/scripts/dynastub/*.sh)
    │
    ▼
Sidecar 检测到脚本变化
    │
    ▼
Sidecar 重新复制脚本到 emptyDir Volume
    │
    ▼
主容器下次执行命令时，新脚本生效
```

## 模式说明

### Local 模式

Sidecar 注入到目标 Pod 中，直接拦截和替换可执行文件。

**适用场景**：
- Pod 内部执行的命令需要行为注入
- 需要低延迟的命令拦截

### Remote 模式（TODO）

创建独立的 FakePod，业务 Pod 通过 SSH 连接到 FakePod 执行命令。

**适用场景**：
- 业务 Pod 通过 SSH 到远程节点执行命令
- 需要集中管理行为注入服务

## 开发指南

### 本地开发

```bash
# 安装依赖
go mod tidy

# 本地运行（需要配置 KUBECONFIG）
go run ./cmd/main.go
```

### 构建镜像

```bash
# 构建 Operator 镜像
docker build -t dynastub:latest .

# 构建 Sidecar 镜像
docker build -t dynastub-sidecar:latest -f build/Dockerfile.sidecar .

# 推送镜像
docker push your-registry/dynastub:latest
docker push your-registry/dynastub-sidecar:latest
```

### 生成代码

```bash
# 生成 DeepCopy 方法
go run sigs.k8s.io/controller-tools/cmd/controller-gen@latest object paths=./api/v1/

# 生成 CRD
go run sigs.k8s.io/controller-tools/cmd/controller-gen@latest crd paths=./api/v1/ output:crd:dir=./config/crd/bases
```

## 故障排查

### 常见问题

| 问题 | 可能原因 | 解决方案 |
|------|---------|---------|
| Pod 未被注入 | 缺少 `dynastub-inject=enabled` 标签 | 添加标签后重新创建 Pod |
| 注入未生效 | Sidecar 未就绪 | 检查 sidecar 日志和 ready marker |
| 脚本执行失败 | 脚本路径错误或权限问题 | 检查 hostPath 挂载和脚本权限 |
| 脚本未更新 | Sidecar 未检测到变化 | 检查 sidecar 日志和文件监控 |

### 排查命令

```bash
# 查看 Operator 日志
kubectl logs -l app.kubernetes.io/name=dynastub -n <namespace>

# 查看 BehaviorStub 状态
kubectl get behaviorstub -A
kubectl describe behaviorstub <name>

# 查看 Pod 注入情况
kubectl get pod <pod-name> -o yaml | grep -A 20 initContainers

# 查看生成的脚本
kubectl exec <pod-name> -c <main-container> -- cat /usr/bin/docker

# 查看注入日志
kubectl exec <pod-name> -c <main-container> -- cat /tmp/dynastub.log
```

## 高级扩展场景

基于 DynaStub 的核心能力，还可扩展到以下高级场景：

### 1. 动态环境变量/配置覆盖
- **场景**：动态修改容器内环境变量或配置文件，无需重启 Pod
- **实现**：Webhook 注入 Sidecar 并共享 Volume，CRD 定义环境变量覆盖规则
- **价值**：快速开启 debug 日志、修改 feature flag，适合运行时可动态 reload 的应用

### 2. 命令执行流量的录制与回放
- **场景**：录制生产环境容器内执行的命令及其参数、输出、退出码
- **实现**：Webhook 注入 Sidecar 包装系统命令，录制命令信息并支持回放
- **价值**：命令级录制回放，适合 CLI 工具、运维脚本的混沌测试和回归测试

### 3. 动态资源限制模拟
- **场景**：测试应用在容器内存/CPU 被限制时的行为
- **实现**：Webhook 注入 Sidecar，生成脚本覆盖资源限制相关命令，返回伪造的数值
- **价值**：在不改变 Pod 真实 QoS 的前提下，测试应用的资源限制感知逻辑

### 4. 高级网络操作
- **场景**：除 DNS 劫持外的网络流量管理，如流量镜像、延迟注入、丢包模拟等
- **实现**：Webhook 注入 Sidecar，覆盖网络相关命令，实现高级网络操作
- **价值**：无侵入的网络行为模拟，适合网络相关的混沌测试和性能测试

## 许可证

MIT License

Copyright (c) 2024 DynaStub

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
