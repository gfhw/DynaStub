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

## 功能特性

- **用户编写脚本**：用户直接编写包装脚本，无需学习复杂规则语法
- **CR 驱动**：通过自定义资源（CR）声明式定义注入规则
- **动态热更新**：脚本变更秒级生效，无需重启 Pod
- **动态热更新**：脚本变更秒级生效，无需重启 Pod
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

### 用户脚本编写指南

用户需要编写包装脚本，脚本应该：

1. **处理参数**：接收所有命令行参数
2. **实现逻辑**：根据参数执行自定义逻辑
3. **日志记录**：可选记录命令执行信息

### 完整配置示例

```yaml
apiVersion: dynastub.example.com/v1
kind: BehaviorStub
metadata:
  name: example-stub
  namespace: admin
spec:
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

### 多命令打桩支持

`behaviors` 数组支持同时定义多个命令打桩规则：

```yaml
behaviors:
  - name: docker
    targetPath: /usr/bin/docker
    scriptPath: /data/scripts/docker-wrapper.sh
  - name: kubectl
    targetPath: /usr/local/bin/kubectl
    scriptPath: /data/scripts/kubectl-wrapper.sh
  - name: curl
    targetPath: /usr/bin/curl
    scriptPath: /data/scripts/curl-wrapper.sh
```

**实现机制**：
- Sidecar 根据 `behaviors` 列表，将每个 `scriptPath` 的脚本复制到 `emptyDir` Volume
- 目标文件名使用 `targetPath` 的最后一部分（如 `/usr/bin/docker` → `docker`）
- Webhook 为每个 behavior 在主容器中创建对应的 `subPath` VolumeMount
- 每个命令独立挂载，互不影响

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

## 开发指南

### 本地开发

```bash
# 安装依赖
go mod tidy

# 本地运行（需要配置 KUBECONFIG）
go run ./cmd/main.go
```

### 构建镜像

#### 1. 编译二进制文件

```bash
# 编译 Operator Manager（Linux x86）
go build -o build/manager -ldflags="-s -w" -tags=netgo ./cmd/main.go

# 编译 Sidecar（Linux x86）  
go build -o build/sidecar -ldflags="-s -w" -tags=netgo ./cmd/sidecar/main.go

# 交叉编译（如果需要在不同平台编译）
GOOS=linux GOARCH=amd64 go build -o build/manager-linux-x86 -ldflags="-s -w" -tags=netgo ./cmd/main.go
GOOS=linux GOARCH=amd64 go build -o build/sidecar-linux-x86 -ldflags="-s -w" -tags=netgo ./cmd/sidecar/main.go
```

#### 2. 构建 Docker 镜像

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

## 技术栈亮点

DynaStub 项目展示了完整的 Kubernetes Operator 开发能力，涵盖了以下核心技术栈：

### 🎯 核心技术栈

#### 1. Operator 开发框架
- **Kubebuilder/Controller-Runtime**：标准的 Operator SDK 框架
- **CRD 自定义资源**：BehaviorStub 资源定义和 API 管理
- **Controller 模式**：完整的 Reconcile 循环和状态管理
- **RBAC 权限控制**：精细的权限配置和安全管理

#### 2. Webhook 机制
- **MutatingWebhookConfiguration**：Pod 创建时自动注入 Sidecar
- **TLS 证书动态管理**：自动生成包含正确 DNS SAN 的证书
- **准入控制**：在 Pod 创建时进行拦截和修改
- **证书轮换机制**：支持证书更新和安全管理

#### 3. Helm 部署体系
- **完整的 Chart 打包**：支持多环境部署和配置管理
- **Helm Hook 自动化**：预安装/预升级钩子实现证书自动生成
- **Values 配置管理**：灵活的配置参数和环境适配
- **模板化部署**：支持多租户和动态配置

#### 4. 证书管理
- **动态 TLS 证书生成**：使用 Go 标准库生成服务器证书
- **DNS SAN 配置**：自动配置正确的 DNS 名称（多租户支持）
- **Secret 安全存储**：Kubernetes Secret 安全存储证书
- **证书验证机制**：证书有效性检查和自动更新

#### 5. 边车模式
- **Sidecar 容器注入**：动态注入到目标 Pod
- **双模式设计**：证书生成模式 + 文件拷贝模式
- **资源隔离**：独立的容器运行环境
- **原生 Sidecar 支持**：利用 Kubernetes 1.28+ 特性确保初始化顺序

#### 6. Job 和 Hook 机制
- **Kubernetes Job**：证书生成任务的执行和管理
- **Hook 生命周期**：部署生命周期的自动化管理
- **服务账户权限**：精细的 RBAC 配置和权限控制
- **故障恢复机制**：任务失败的重试和恢复策略

### 🌟 项目亮点

- **架构先进**：Operator + Webhook + Sidecar 的完整架构设计
- **工程化实践**：完整的 CI/CD 支持，配置管理，故障恢复机制
- **安全性设计**：完整的 TLS 证书管理，RBAC 权限控制
- **多租户支持**：动态证书生成，支持多环境部署

### 与业界工具对比

### 现有工具分析

目前业界针对 Kubernetes 的命令/行为拦截工具主要集中在以下方向：

| 工具类型 | 代表工具 | 工作原理 | 局限性 |
|---------|---------|---------|--------|
| **DynaStub** | - | 命令级拦截（emptyDir + subPath 挂载） | 仅支持文件系统命令，不支持网络流量 |
| **Service Mesh** | Istio, Linkerd | 网络层拦截（Sidecar Proxy） | 只能拦截网络流量，无法拦截文件系统命令 |
| **eBPF 工具** | Falco, Tetragon | 内核事件监控 | 主要用于安全审计，难以实现命令替换和自定义行为 |
| **Chaos Engineering** | Chaos Mesh, Litmus | 故障注入 | 需要特定 CRD，无法灵活自定义脚本逻辑 |
| **Admission Controller** | OPA Gatekeeper | 策略控制 | 只能拦截和拒绝，无法实现命令替换 |

### DynaStub 的独特性

**目前业界尚无与 DynaStub 完全类似的工具**，主要原因：

1. **命令级拦截**：现有工具主要关注网络层或系统调用层，没有专门针对容器内命令替换的解决方案
2. **用户脚本驱动**：DynaStub 允许用户直接编写 Shell 脚本实现任意逻辑，而非受限于预设的规则 DSL
3. **无侵入热更新**：无需修改业务镜像、无需重启 Pod，脚本变更秒级生效
4. **Kubernetes 原生**：基于 Operator + Webhook 架构，完全云原生

### 适用场景对比

| 场景 | DynaStub | Service Mesh | eBPF 工具 | Chaos 工具 |
|------|----------|-------------|-----------|------------|
| 文件系统命令拦截 | ✅ | ❌ | ⚠️ | ❌ |
| 自定义脚本逻辑 | ✅ | ❌ | ❌ | ⚠️ |
| 热更新（无需重启） | ✅ | ✅ | ✅ | ⚠️ |
| 故障注入 | ✅ | ⚠️ | ❌ | ✅ |
| 命令审计 | ✅ | ❌ | ✅ | ❌ |
| 易用性（脚本编写） | ✅ | ⚠️ | ❌ | ⚠️ |
| 学习成本 | 低 | 高 | 高 | 中 |

**说明**：
- ✅ 完全支持
- ⚠️ 部分支持或需要额外配置
- ❌ 不支持

### 结论

DynaStub 填补了业界在 **Kubernetes 容器内命令行为动态拦截** 领域的空白，特别适合需要：
- 灵活自定义命令行为的场景
- 快速迭代测试脚本的开发环境
- 无法修改业务镜像的遗留系统

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
