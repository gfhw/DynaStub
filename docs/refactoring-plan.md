# DynaStub 代码库改造计划

## 一、现状分析

### 1.1 当前代码库架构

当前代码库是一个 **HTTP 打桩服务**，基于以下技术栈：

| 组件 | 当前实现 | 用途 |
|------|---------|------|
| **CRD** | `HTTPTestStub` | 定义 HTTP 请求/响应匹配规则 |
| **Controller** | `HTTPTestStubReconciler` | 监听 CR 变化，缓存到内存 |
| **HTTP 服务** | Beego 框架 | 提供 HTTP/HTTPS 打桩服务 |
| **请求处理** | `StubController` | 匹配请求并返回模拟响应 |
| **脚本执行** | `ScriptExecutor` | 执行 shell/python 脚本生成响应 |

### 1.2 核心文件分析

```
.
├── api/v1/
│   ├── http_test_stub_types.go      # CRD 定义：HTTP 请求/响应匹配
│   ├── groupversion_info.go
│   └── zz_generated.deepcopy.go
├── internal/controller/
│   ├── http_test_stub_controller.go # Controller：缓存 CR 到内存
│   ├── beego_controllers.go         # Beego HTTP 服务处理
│   └── script_executor.go           # 脚本执行器
├── cmd/main.go                      # 入口：启动 Manager + Beego HTTP 服务
└── charts/                          # Helm Chart 部署配置
```

### 1.3 当前数据流

```
用户请求 → Beego HTTP 服务 → 内存缓存匹配 → 返回模拟响应
                ↑
           CRD (HTTPTestStub)
                ↑
           Controller 监听
```

## 二、目标架构分析

### 2.1 目标架构（DynaStub 动态行为注入框架）

基于 README 描述，目标架构是 **Kubernetes 动态行为注入框架**：

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

### 2.2 核心差异对比

| 维度 | 当前实现 | 目标实现 |
|------|---------|---------|
| **核心功能** | HTTP 请求打桩 | 命令行行为注入 |
| **服务类型** | HTTP 服务端 | Kubernetes Operator + Webhook |
| **拦截方式** | HTTP 路由匹配 | 文件系统挂载替换 |
| **响应方式** | HTTP 响应 | 脚本/二进制执行 |
| **Beego 依赖** | 必须（HTTP 服务） | 不需要 |
| **Sidecar** | 无 | 必须 |

## 三、改造计划

### 第一阶段：移除 Beego 框架（立即执行）

**目标**：完全移除 Beego HTTP 服务相关代码

#### 3.1.1 删除文件

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/controller/beego_controllers.go` | **删除** | Beego HTTP 控制器 |

#### 3.1.2 修改文件

**文件：`cmd/main.go`**

- 删除 Beego 相关 import
- 删除 `setupBeego()` 函数
- 删除 HTTP/HTTPS 端口相关 flag
- 删除 `beego.Run()` 调用
- 保留 Manager 启动逻辑

**文件：`go.mod` / `go.sum`**

- 移除 `github.com/beego/beego/v2` 依赖
- 执行 `go mod tidy` 清理

#### 3.1.3 第一阶段验证

```bash
# 编译验证
go build ./cmd/main.go

# 确保没有 Beego 依赖
grep -r "beego" --include="*.go" .
```

---

### 第二阶段：重构 CRD（API 层）

**目标**：将 `HTTPTestStub` 改造为 `BehaviorStub`

#### 3.2.1 新建 CRD 定义

**文件：`api/v1/behavior_stub_types.go`**（新建）

```go
// BehaviorStubSpec 定义行为注入规则
type BehaviorStubSpec struct {
    Mode         string            `json:"mode"` // local, remote
    LocalConfig  *LocalConfig      `json:"localConfig,omitempty"`
    ScriptVolume ScriptVolume      `json:"scriptVolume"`
    Behaviors    []Behavior        `json:"behaviors"`
    Advanced     *AdvancedConfig   `json:"advanced,omitempty"`
}

// LocalConfig Local 模式配置
type LocalConfig struct {
    TargetSelector    metav1.LabelSelector `json:"targetSelector"`
    SidecarImage      string               `json:"sidecarImage"`
    SidecarResources  *corev1.ResourceRequirements `json:"sidecarResources,omitempty"`
}

// ScriptVolume 脚本存储卷配置
type ScriptVolume struct {
    HostPath  string `json:"hostPath"`
    MountPath string `json:"mountPath"`
}

// Behavior 单个行为注入配置
type Behavior struct {
    Name           string `json:"name"`
    TargetPath     string `json:"targetPath"`     // 目标可执行文件路径
    ScriptPath     string `json:"scriptPath"`     // 用户脚本路径（相对于 scriptVolume.mountPath）
    BackupOriginal bool   `json:"backupOriginal"` // 是否备份原命令（默认：true）
    EnableLogging  bool   `json:"enableLogging,omitempty"` // 是否启用日志记录
    LogPath        string `json:"logPath,omitempty"`       // 日志文件路径
}

// AdvancedConfig 高级配置
type AdvancedConfig struct {
    EnableLogging      bool `json:"enableLogging,omitempty"`
    SyncIntervalSeconds int `json:"syncIntervalSeconds,omitempty"`
}
```

#### 3.2.2 删除旧 CRD

- 删除 `api/v1/http_test_stub_types.go`
- 更新 `api/v1/groupversion_info.go` 中的 Group 名称

#### 3.2.3 重新生成代码

```bash
# 生成 DeepCopy
go run sigs.k8s.io/controller-tools/cmd/controller-gen@latest object paths=./api/v1/

# 生成 CRD
go run sigs.k8s.io/controller-tools/cmd/controller-gen@latest crd paths=./api/v1/ output:crd:dir=./config/crd/bases
```

---

### 第三阶段：实现 Mutating Webhook

**目标**：实现 Pod 注入 Webhook，自动注入 Sidecar

#### 3.3.1 新建 Webhook 文件

**文件：`internal/webhook/pod_webhook.go`**（新建）

核心功能：
- 拦截 Pod 创建请求
- 检查标签选择器匹配
- 注入 Sidecar 容器
- 添加 emptyDir Volume
- 添加 VolumeMount（subPath 挂载）

```go
// PodMutator 实现 admission.DecoderInjector
type PodMutator struct {
    Client  client.Client
    Decoder admission.Decoder
}

// Handle 处理 Pod 创建请求
func (m *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
    // 1. 解析 Pod
    // 2. 查询匹配的 BehaviorStub CR
    // 3. 注入 Sidecar 容器
    // 4. 注入 emptyDir Volume
    // 5. 为每个 Behavior 添加 subPath VolumeMount
}
```

#### 3.3.2 Webhook 配置

- 在 `cmd/main.go` 中注册 Webhook
- 配置 Webhook Service 和证书
- 更新 Helm Chart 添加 Webhook 配置

---

### 第四阶段：重构 Controller

**目标**：Controller 管理 Webhook 配置和状态更新

#### 3.4.1 修改 Controller 逻辑

**文件：`internal/controller/behavior_stub_controller.go`**（重构）

新职责：
1. 监听 `BehaviorStub` CR 变化
2. 更新 CR 状态（注入状态、目标 Pod 数量等）
3. 清理已删除 CR 的相关资源

```go
func (r *BehaviorStubReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. 获取 BehaviorStub
    // 2. 根据 targetSelector 查找目标 Pod
    // 3. 更新 CR 状态（注入状态、目标 Pod 数量等）
}
```

#### 3.4.2 删除旧逻辑

- 删除 HTTP 打桩相关的缓存逻辑
- 删除请求匹配逻辑
- 删除 ConfigMap 生成逻辑

---

### 第五阶段：实现 Sidecar 容器

**目标**：Sidecar 监听用户脚本变化并复制到目标位置

#### 3.5.1 新建 Sidecar 代码

**文件：`cmd/sidecar/main.go`**（新建）

核心功能：
1. 读取 CR 配置，获取用户脚本路径
2. 将用户脚本从 hostPath 复制到 emptyDir Volume
3. 确保脚本有执行权限 (0755)
4. 监听用户脚本变化，实时更新
5. 创建 ready marker 文件通知主容器

```go
func main() {
    // 1. 加载初始配置
    config := loadConfig()
    
    // 2. 复制所有用户脚本到目标位置
    copier := NewScriptCopier(config)
    copier.CopyAll()
    
    // 3. 标记 ready
    markReady()
    
    // 4. 启动文件监听，检测脚本变化
    watcher := NewFileWatcher(config)
    watcher.Start()
    
    // 5. 阻塞保持运行
    select {}
}
```

#### 3.5.2 脚本复制器

**文件：`internal/sidecar/copier.go`**（新建）

```go
// ScriptCopier 复制用户脚本到目标位置
type ScriptCopier struct {
    config    *Config
    outputDir string
}

// Copy 复制单个用户脚本
func (c *ScriptCopier) Copy(behavior Behavior) error {
    // 1. 检查用户脚本是否存在
    // 2. 复制脚本到 emptyDir Volume
    // 3. 设置执行权限 (0755)
    // 4. 如果 backupOriginal 为 true，备份原命令
}

// 用户脚本示例：
// #!/bin/bash
// # /home/scripts/dynastub/docker-wrapper.sh
// 
// LOG_FILE=/tmp/dynastub-docker.log
// echo "$(date) - docker $@" >> $LOG_FILE
// 
// # 自定义逻辑
// case "$1" in
//     "ps")
//         # 返回伪造的容器列表
//         echo "CONTAINER ID   IMAGE          STATUS"
//         echo "abc123         myapp:latest   Up 2 hours"
//         exit 0
//         ;;
//     "version")
//         # 返回伪造版本信息
//         echo "Docker version 99.99.99 (DynaStub injected)"
//         exit 0
//         ;;
//     *)
//         # 其他命令透传给原 docker
//         /usr/bin/docker.original "$@"
//         ;;
// esac
```

---

### 第六阶段：更新部署配置

**目标**：更新 Helm Chart 和 Kubernetes 配置

#### 3.6.1 更新 Helm Chart

**文件：`charts/dynastub/values.yaml`**

```yaml
# Operator 配置
operator:
  image: dynastub-operator:latest
  resources: {}
  
# Webhook 配置
webhook:
  enabled: true
  certManager:
    enabled: false  # 或使用 cert-manager
    
# Sidecar 镜像配置
sidecar:
  image: dynastub-sidecar:latest
  resources:
    requests:
      cpu: 50m
      memory: 32Mi
```

**文件：`charts/dynastub/templates/webhook.yaml`**（新建）

- MutatingWebhookConfiguration
- Service
- 证书配置（Secret 或 cert-manager）

**文件：`charts/dynastub/templates/deployment.yaml`**

- 添加 Webhook 端口
- 添加卷挂载（证书）

#### 3.6.2 更新 Dockerfile

**文件：`build/Dockerfile`**（修改）
- 移除 Beego 相关配置
- 添加 Webhook 证书路径

**文件：`build/Dockerfile.sidecar`**（新建）
- 基于 alpine 或 scratch
- 只包含 sidecar 二进制

---

### 第七阶段：测试与验证

#### 3.7.1 单元测试

- Controller 测试
- Webhook 测试
- Sidecar 脚本生成测试

#### 3.7.2 集成测试

```bash
# 1. 部署 Operator
helm install dynastub ./charts/dynastub

# 2. 创建测试 Pod
kubectl apply -f test/fixtures/test-pod.yaml

# 3. 创建 BehaviorStub
kubectl apply -f test/fixtures/behavior-stub.yaml

# 4. 验证注入
kubectl get pod test-pod -o yaml
kubectl exec test-pod -c main -- which docker
kubectl exec test-pod -c main -- docker ps
```

#### 3.7.3 E2E 测试

- 使用 kind 或真实集群
- 验证完整流程：CR 创建 → Webhook 注入 → Sidecar 生成脚本 → 命令拦截

---

## 四、文件变更清单

### 4.1 删除文件

| 文件路径 | 说明 |
|---------|------|
| `internal/controller/beego_controllers.go` | Beego HTTP 控制器 |
| `api/v1/http_test_stub_types.go` | 旧 CRD 定义 |
| `config/crd/bases/_httpteststubs.yaml` | 旧 CRD YAML |

### 4.2 新建文件

| 文件路径 | 说明 |
|---------|------|
| `api/v1/behavior_stub_types.go` | 新 CRD 定义 |
| `internal/webhook/pod_webhook.go` | Pod 注入 Webhook |
| `cmd/sidecar/main.go` | Sidecar 入口 |
| `internal/sidecar/copier.go` | 脚本复制器 |
| `internal/sidecar/watcher.go` | 文件监听器 |
| `build/Dockerfile.sidecar` | Sidecar 镜像构建 |
| `charts/dynastub/templates/webhook.yaml` | Webhook 配置 |

### 4.3 修改文件

| 文件路径 | 修改内容 |
|---------|---------|
| `cmd/main.go` | 移除 Beego，添加 Webhook 注册 |
| `internal/controller/http_test_stub_controller.go` | 重命名为 `behavior_stub_controller.go`，重构逻辑 |
| `api/v1/groupversion_info.go` | 更新 Group 名称 |
| `PROJECT` | 更新项目配置 |
| `go.mod` | 移除 Beego 依赖 |
| `charts/dynastub/values.yaml` | 更新配置值 |
| `charts/dynastub/templates/deployment.yaml` | 添加 Webhook 支持 |
| `build/Dockerfile` | 更新构建配置 |

---

## 五、实施建议

### 5.1 推荐实施顺序

```
第一阶段 ──→ 第二阶段 ──→ 第三阶段 ──→ 第四阶段 ──→ 第五阶段 ──→ 第六阶段 ──→ 第七阶段
(移除Beego)  (重构CRD)   (Webhook)   (Controller)  (Sidecar)   (部署配置)   (测试)
```

### 5.2 风险与注意事项

| 风险 | 缓解措施 |
|------|---------|
| Webhook 证书管理复杂 | 使用 cert-manager 或预生成证书 |
| Sidecar 与主容器启动顺序 | 使用 K8s 1.28+ 原生 Sidecar 或 Init Container |
| 脚本权限问题 | 确保生成的脚本有执行权限 (0755) |
| 原文件备份恢复 | 实现 backupOriginal 逻辑，保留原文件 |
| 性能影响 | Sidecar 资源限制，脚本生成优化 |

### 5.3 里程碑设定

| 里程碑 | 交付物 | 验收标准 |
|--------|--------|---------|
| M1 | 移除 Beego 的代码库 | 编译通过，无 Beego 依赖 |
| M2 | 新 CRD + Webhook | 可部署，Webhook 可拦截 Pod |
| M3 | Sidecar 基础功能 | Sidecar 可生成脚本 |
| M4 | 完整功能 | 端到端测试通过 |
| M5 | 文档完善 | README + 使用文档 |

---

## 六、附录

### 6.1 参考资源

- [Kubernetes Mutating Admission Webhook](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)
- [Kubebuilder Webhook 文档](https://book.kubebuilder.io/cronjob-tutorial/webhook-implementation.html)
- [Kubernetes Sidecar 容器](https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/)

### 6.2 关键 Kubernetes API

```go
// Pod 注入核心逻辑
pod.Spec.Containers = append(pod.Spec.Containers, sidecarContainer)
pod.Spec.Volumes = append(pod.Spec.Volumes, emptyDirVolume)

// 为主容器添加 subPath 挂载
for i := range pod.Spec.Containers {
    if pod.Spec.Containers[i].Name == targetContainer {
        pod.Spec.Containers[i].VolumeMounts = append(
            pod.Spec.Containers[i].VolumeMounts,
            corev1.VolumeMount{
                Name:      "dynastub-scripts",
                MountPath: "/usr/bin/docker",
                SubPath:   "docker",
            },
        )
    }
}
```

---

**文档版本**: v1.0  
**创建日期**: 2026-04-06  
**作者**: AI Assistant
