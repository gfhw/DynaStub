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
│  │  Controller  │  │   Webhook    │  │  ConfigMap       │  │
│  │  (Watch CR)  │  │ (Pod注入)    │  │  (规则存储)      │  │
│  └──────┬───────┘  └──────┬───────┘  └────────┬─────────┘  │
│         │                 │                    │            │
│         ▼                 ▼                    ▼            │
│  ┌──────────────────────────────────────────────────────┐  │
│  │                    Sidecar Container                  │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌────────────┐  │  │
│  │  │ Config Watch │  │ Script Gen   │  │ emptyDir   │  │  │
│  │  │ (监听规则)   │  │ (生成脚本)   │  │ Volume     │  │  │
│  │  └──────────────┘  └──────────────┘  └─────┬──────┘  │  │
│  └─────────────────────────────────────────────┼─────────┘  │
│                                                │            │
│  ┌─────────────────────────────────────────────▼─────────┐  │
│  │  Target Pod (业务容器)                                 │  │
│  │  ┌──────────────┐  ┌──────────────┐                  │  │
│  │  │ 可执行文件 (subPath挂载)│  │ 其他原生命令    │  │
│  │  │ → 行为注入脚本 │  │ (未注入)     │  │
│  │  └──────────────┘  └──────────────┘                  │  │
│  └─────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### 核心组件

1. **Controller**：监听 CR 变化，管理 ConfigMap 和 Webhook 配置
2. **Webhook**：拦截 Pod 创建请求，注入 Sidecar 容器和相关配置
3. **Sidecar**：动态生成行为注入脚本，监听配置变更，实现热更新
4. **ConfigMap**：存储规则配置，作为 Controller 和 Sidecar 之间的通信桥梁

## 技术架构

DynaStub 设计并实现了基于 Operator + Mutating Webhook + Sidecar 的无重启命令拦截架构，支持秒级热更新。

### 关键技术

- **K8s Operator**：基于 Controller-Runtime 框架，实现声明式资源管理
- **Mutating Webhook**：拦截 Pod 创建请求，自动注入 Sidecar 容器
- **原生 Sidecar**：利用 Kubernetes 1.28+ 原生 Sidecar 特性，确保初始化顺序
- **emptyDir + subPath 动态挂载**：实现脚本的实时更新和命令拦截
- **Shell 脚本原子生成**：Sidecar 动态生成行为注入脚本，无需编译二进制

## 功能特性

- **零编译**：Sidecar 动态生成 Shell 脚本，无需编译二进制文件
- **CR 驱动**：通过自定义资源（CR）声明式定义规则
- **动态热更新**：规则变更秒级生效，无需重启 Pod
- **双模式支持**：Local 模式（注入到目标 Pod）和 Remote 模式（独立 FakePod）
- **灵活匹配**：支持精确匹配、通配符匹配和正则表达式匹配
- **多种响应**：支持静态响应、脚本执行、内联脚本和透传模式
- **云原生**：完整的 Helm Chart 支持，基于 Kubernetes Operator 模式

## 应用场景

### 1. 安全：命令审计与拦截

**场景**：容器内执行高危命令时，需要实时审计、告警或直接拦截，且策略可动态更新。

**实现方式**：
- CRD 定义安全规则
- Webhook 注入 Sidecar，挂载关键命令
- Sidecar 生成包装脚本，实现审计和拦截逻辑

**价值**：无需修改业务镜像，即可实现零信任容器安全防线，适合多租户集群的安全合规场景。

### 2. 测试：故障注入与混沌工程

**场景**：测试应用对文件系统错误、网络超时、磁盘满、命令返回错误码的容错能力。

**实现方式**：
- CRD 定义故障注入规则
- Sidecar 生成脚本注入人为延迟、修改返回内容、随机返回错误
- 支持状态化故障和概率性故障

**价值**：实现无侵入、秒级生效的系统调用级故障模拟，特别适合存储、网络、系统命令依赖重的应用。

### 3. 运维：动态配置与监控注入

**场景**：需要动态修改容器内环境变量或配置文件，或注入日志轮转、监控探针等运维工具。

**实现方式**：
- Webhook 根据标签自动注入 Sidecar
- Sidecar 生成配置脚本，实现环境变量覆盖
- 共享 Volume 实现日志轮转和监控数据收集

**价值**：实现声明式运维注入，集群管理员只需定义规则，符合条件的 Pod 自动获得相应能力。

### 4. 兼容性：命令适配与环境变量覆盖

**场景**：业务代码依赖特定版本的命令参数，但基础镜像中版本过新/过旧，导致不兼容。

**实现方式**：
- CRD 定义适配规则
- Sidecar 生成命令包装脚本，解析并转换参数
- 热更新适配规则，无需重建 Pod

**价值**：解决云原生场景下"基础镜像版本锁死"与"业务新特性需要新命令"之间的矛盾。

### 5. 网络：DNS 劫持与流量镜像

**场景**：需要将容器内对某个域名的解析临时指向特定 IP，或镜像网络流量用于分析。

**实现方式**：
- Webhook 注入 Sidecar，覆盖网络相关命令
- Sidecar 生成包装脚本，实现 DNS 解析劫持
- 支持流量镜像和分析

**价值**：实现无重启的 DNS 劫持，适合多环境联调、A/B 测试、服务迁移验证等场景。

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
      backupOriginal: false
      rules:
        - name: docker-ps
          match:
            type: exact
            args: ["ps", "-a"]
          action:
            type: response
            response:
              stdout: |
                CONTAINER ID   IMAGE          STATUS
                abc123         myapp:latest   Up 2 hours
              exitCode: 0
        - name: docker-save
          match:
            type: glob
            pattern: "save -o *"
          action:
            type: scriptPath
            scriptPath: /data/scripts/handle-docker-save.sh
      defaultAction:
        type: response
        response:
          stdout: "Command not handled"
          exitCode: 1
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

### 匹配类型

| 类型 | 说明 | 示例 |
|------|------|------|
| `exact` | 精确匹配参数列表 | `args: ["ps", "-a"]` |
| `glob` | 通配符匹配 | `pattern: "save -o *"` |
| `regex` | 正则表达式匹配 | `pattern: "^load -i (.+)$"` |

### 动作类型

| 类型 | 说明 | 使用场景 |
|------|------|----------|
| `response` | 返回固定的 stdout/stderr/exitCode | 简单固定响应 |
| `scriptPath` | 执行指定路径的脚本 | 复杂业务逻辑 |
| `inlineScript` | 执行内联脚本 | 简单脚本逻辑 |
| `passthrough` | 透传给原始命令 | 保留某些行为 |

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
      backupOriginal: false
      rules:
        - name: docker-ps
          match:
            type: exact
            args: ["ps", "-a"]
          action:
            type: response
            response:
              stdout: |
                CONTAINER ID   IMAGE          STATUS
                abc123         myapp:latest   Up 2 hours
              exitCode: 0
        - name: docker-save
          match:
            type: glob
            pattern: "save -o *"
          action:
            type: scriptPath
            scriptPath: /data/scripts/handle-docker-save.sh
        - name: docker-version
          match:
            type: exact
            args: ["version"]
          action:
            type: passthrough
      defaultAction:
        type: response
        response:
          exitCode: 0

    - name: chown
      targetPath: /usr/bin/chown
      backupOriginal: true
      rules:
        - name: chown-any
          match:
            type: glob
            pattern: "*"
          action:
            type: inlineScript
            inlineScript: |
              #!/bin/sh
              echo "$(date) chown $@" >> /tmp/dynastub.log
              exit 0
      defaultAction:
        type: response
        response:
          exitCode: 0

  advanced:
    enableLogging: true
    logPath: /tmp/dynastub.log
    syncIntervalSeconds: 5
```

## 工作原理

### 1. Sidecar 脚本生成

Sidecar 根据 CR 中的规则，为每个目标可执行文件动态生成行为注入脚本。

### 2. 命令拦截机制

使用 Kubernetes 的 `emptyDir` Volume + `subPath` 挂载实现命令遮盖，确保目标可执行文件被注入的脚本替换。

### 3. 动态更新流程

```
用户修改 CR (kubectl edit behaviorstub xxx)
    │
    ▼
Controller 检测到变更，更新 ConfigMap
    │
    ▼
Sidecar Watch 到 ConfigMap 变化
    │
    ▼
Sidecar 重新生成所有行为注入脚本
    │
    ▼
主容器下次执行命令时，新规则生效
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
| ConfigMap 未创建 | Controller 异常 | 查看 Operator 日志 |

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

## 扩展场景

基于 DynaStub 的核心能力，可扩展到以下场景：

### 1. 动态安全策略执行
- **场景**：容器内执行高危命令时的实时审计、告警或拦截
- **实现**：CRD 定义安全规则，Webhook 注入 sidecar 挂载关键命令，生成包装脚本实现审计和拦截
- **价值**：零信任容器安全防线，适合多租户集群的安全合规场景

### 2. 故障注入与混沌工程
- **场景**：测试应用对文件系统错误、网络超时、磁盘满等异常的容错能力
- **实现**：CRD 定义故障注入规则，sidecar 生成脚本注入人为延迟、修改返回内容、随机返回错误
- **价值**：无侵入、秒级生效的系统调用级故障模拟

### 3. 多版本命令兼容性适配层
- **场景**：解决业务代码与基础镜像中命令版本不兼容问题
- **实现**：CRD 定义适配规则，sidecar 生成命令包装脚本，解析并转换参数
- **价值**：解决"基础镜像版本锁死"与"业务新特性需要新命令"之间的矛盾

### 4. 动态注入运维 Sidecar
- **场景**：为业务容器注入日志轮转、监控探针、流量代理等运维工具
- **实现**：Webhook 根据标签自动注入 sidecar，共享 Volume 实现日志轮转和监控数据收集
- **价值**：实现声明式运维注入，集群管理员只需定义规则

### 5. 动态环境变量/配置覆盖
- **场景**：动态修改容器内环境变量或配置文件，无需重启 Pod
- **实现**：Webhook 注入 sidecar 并共享 Volume，CRD 定义环境变量覆盖规则
- **价值**：快速开启 debug 日志、修改 feature flag，适合运行时可动态 reload 的应用

### 6. 命令执行流量的录制与回放
- **场景**：录制生产环境容器内执行的命令及其参数、输出、退出码
- **实现**：Webhook 注入 sidecar 包装系统命令，录制命令信息并支持回放
- **价值**：命令级录制回放，适合 CLI 工具、运维脚本的混沌测试和回归测试

### 7. 动态资源限制模拟
- **场景**：测试应用在容器内存/CPU 被限制时的行为
- **实现**：Webhook 注入 sidecar，生成脚本覆盖资源限制相关命令，返回伪造的数值
- **价值**：在不改变 Pod 真实 QoS 的前提下，测试应用的资源限制感知逻辑

### 8. 动态 DNS/网络劫持
- **场景**：临时修改容器内域名解析，指向特定 IP
- **实现**：Webhook 注入 sidecar，覆盖网络相关命令，实现 DNS 解析劫持
- **价值**：无重启的 DNS 劫持，适合多环境联调、A/B 测试、服务迁移验证

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
