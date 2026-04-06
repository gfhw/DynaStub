# DynaStub - Kubernetes 动态命令打桩框架

## 一、架构概述

DynaStub 是一个 Kubernetes Operator，用于在容器内动态替换可执行文件（命令），实现命令行为的拦截和自定义。它通过 Sidecar 模式将用户脚本注入到目标 Pod 中，无需修改业务镜像即可实现命令打桩。

## 二、核心组件

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Kubernetes Cluster                                  │
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                    DynaStub Operator                                │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌────────────────────────────┐  │   │
│  │  │ Controller  │  │  Webhook    │  │      BehaviorStub CRD      │  │   │
│  │  │ (状态管理)  │  │ (Pod 注入)  │  │  (用户声明注入规则)         │  │   │
│  │  └──────┬──────┘  └──────┬──────┘  └────────────────────────────┘  │   │
│  │         │                │                                         │   │
│  │         │                │ 拦截 Pod 创建请求                        │   │
│  │         │                ▼                                         │   │
│  │         │         匹配 BehaviorStub                                │   │
│  │         │         注入 Sidecar + Volume                            │   │
│  │         │                                                          │   │
│  └─────────┼──────────────────────────────────────────────────────────┘   │
│            │                                                                │
│            │ 注入                                                            │
│            ▼                                                                │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                        Target Pod                                   │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │              Sidecar Container (initContainer)              │   │   │
│  │  │  - 监听 BehaviorStub CR 变化 (通过 K8s API)                  │   │   │
│  │  │  - 从 hostPath 读取用户脚本                                  │   │   │
│  │  │  - 原子复制脚本到 emptyDir                                   │   │   │
│  │  │  - 持续监听文件变化，支持热更新                               │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │                   业务容器 (Main Container)                  │   │   │
│  │  │                                                             │   │   │
│  │  │  原始命令路径 ──► emptyDir 脚本 (subPath 挂载)               │   │   │
│  │  │                                                             │   │   │
│  │  │  例如: /usr/bin/docker ──► 用户自定义脚本                    │   │   │
│  │  │                                                             │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                                                                     │   │
│  │  Volume: emptyDir (dynastub-shared) ── 存放生效的脚本              │   │
│  │  Volume: hostPath (dynastub-scripts) ── 用户脚本源目录 (只读)       │   │
│  │                                                                     │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.1 组件职责

| 组件 | 职责 |
|------|------|
| **BehaviorStub CRD** | 用户声明注入规则：目标 Pod 选择器、要替换的命令、脚本路径等 |
| **Controller** | 监听 CR 变化，更新注入状态（已注入 Pod 数、总目标 Pod 数等） |
| **Mutating Webhook** | 拦截 Pod 创建请求，根据匹配的 BehaviorStub 动态注入 Sidecar 和 Volume |
| **Sidecar** | 运行在目标 Pod 中，负责从 hostPath 读取用户脚本并复制到 emptyDir，支持热更新 |
| **emptyDir** | 存放最终生效的脚本文件，供业务容器通过 subPath 挂载 |
| **hostPath** | 节点上的目录，存放用户编写的原始脚本（只读挂载到 Sidecar） |

## 三、端到端工作流程

### 3.1 准备阶段

#### 3.1.1 用户准备脚本

在集群节点上准备脚本目录（例如 `/opt/dynastub/scripts/`）：

```bash
mkdir -p /opt/dynastub/scripts/

# 创建 docker 包装脚本
cat > /opt/dynastub/scripts/docker-wrapper.sh << 'EOF'
#!/bin/bash
# DynaStub 用户自定义脚本示例

LOG_FILE="/tmp/dynastub-docker.log"
echo "[$(date)] docker $@" >> "$LOG_FILE"

# 自定义逻辑
case "$1" in
    "ps")
        # 返回伪造的容器列表
        echo "CONTAINER ID   IMAGE          STATUS"
        echo "abc123         myapp:latest   Up 2 hours"
        exit 0
        ;;
    "version")
        echo "Docker version 99.99.99 (DynaStub injected)"
        exit 0
        ;;
    *)
        # 注意：由于 subPath 挂载会覆盖原命令，无法直接透传
        # 如需透传，请在镜像构建时将原命令备份为 .original
        echo "Command intercepted by DynaStub: docker $@"
        exit 1
        ;;
esac
EOF

chmod +x /opt/dynastub/scripts/docker-wrapper.sh
```

#### 3.1.2 部署 Operator

```bash
helm install dynastub ./charts/k8s-http-fake-operator
```

Helm 会创建：
- BehaviorStub CRD
- Operator Deployment（包含 Controller 和 Webhook）
- MutatingWebhookConfiguration
- RBAC 权限

### 3.2 配置阶段

#### 3.2.1 创建 BehaviorStub

```yaml
apiVersion: dynastub.example.com/v1
kind: BehaviorStub
metadata:
  name: docker-stub
  namespace: default
spec:
  mode: local
  localConfig:
    targetSelector:
      matchLabels:
        app: myapp
    sidecarImage: dynastub-sidecar:latest
    sidecarResources:
      limits:
        cpu: "200m"
        memory: "128Mi"
  scriptVolume:
    hostPath: /opt/dynastub/scripts
    mountPath: /src/scripts
  behaviors:
    - name: docker
      targetPath: /usr/bin/docker
      scriptPath: /src/scripts/docker-wrapper.sh
      enableLogging: true
      logPath: /var/log/dynastub/docker.log
```

```bash
kubectl apply -f behaviorstub.yaml
```

### 3.3 注入阶段

#### 3.3.1 创建目标 Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: myapp-pod
  labels:
    app: myapp
spec:
  containers:
    - name: main
      image: myapp:latest
```

```bash
kubectl apply -f pod.yaml
```

#### 3.3.2 Webhook 拦截与注入

当 Pod 创建请求到达 API Server 时：

1. **Mutating Webhook 拦截**
   - Webhook 检查 Pod 标签是否匹配 BehaviorStub 的 `targetSelector`
   - 如果匹配，开始注入流程

2. **注入 Sidecar 容器**
   ```yaml
   initContainers:
     - name: dynastub-sidecar
       image: dynastub-sidecar:latest
       restartPolicy: Always  # K8s 1.28+ 原生 sidecar 模式
       volumeMounts:
         - name: dynastub-shared
           mountPath: /shared
         - name: dynastub-scripts
           mountPath: /src/scripts
           readOnly: true
       env:
         - name: BEHAVIOR_STUB_NAME
           value: "docker-stub"
         - name: BEHAVIOR_STUB_NAMESPACE
           value: "default"
   ```

3. **注入 Volume**
   ```yaml
   volumes:
     - name: dynastub-shared
       emptyDir: {}
     - name: dynastub-scripts
       hostPath:
         path: /opt/dynastub/scripts
   ```

4. **修改业务容器挂载**
   ```yaml
   containers:
     - name: main
       volumeMounts:
         - name: dynastub-shared
           mountPath: /usr/bin/docker
           subPath: docker-wrapper.sh
   ```

5. **返回修改后的 Pod 定义**
   - API Server 继续创建 Pod

### 3.4 运行时阶段

#### 3.4.1 Pod 启动顺序

```
1. kubelet 创建 Volume
   ├── emptyDir (dynastub-shared) - 初始为空
   └── hostPath (dynastub-scripts) - 挂载节点脚本目录

2. 启动 Sidecar 容器 (initContainer)
   ├── 从 K8s API 获取 BehaviorStub 配置
   ├── 从 hostPath 读取用户脚本
   ├── 原子复制脚本到 emptyDir
   │   ├── 创建临时文件 docker-wrapper.sh.tmp
   │   ├── 写入内容
   │   ├── 设置权限 0755
   │   └── 重命名为 docker-wrapper.sh
   ├── 启动文件监听 (5秒轮询)
   └── 标记 ready

3. 启动业务容器
   ├── 挂载 emptyDir 到 /usr/bin/docker
   └── 执行用户定义的启动命令
```

#### 3.4.2 命令拦截流程

当业务容器执行 `docker ps` 时：

```
业务容器
    │
    ▼
/usr/bin/docker (实际指向 emptyDir 中的脚本)
    │
    ▼
docker-wrapper.sh 执行
    │
    ├── 记录日志
    ├── 执行自定义逻辑 (返回伪造数据)
    └── 或透传给 /usr/bin/docker.original
```

### 3.5 热更新阶段

#### 3.5.1 用户修改脚本

```bash
# 编辑节点上的脚本
vim /opt/dynastub/scripts/docker-wrapper.sh

# 添加新逻辑
echo "New logic added"
```

#### 3.5.2 Sidecar 检测变化

Sidecar 每 5 秒检查一次文件修改时间：

```go
// 检测到文件变化
if lastHash != currentHash {
    // 原子复制新脚本到 emptyDir
    atomicCopy(srcPath, dstPath)
    log.Printf("Updated script: %s", scriptName)
}
```

#### 3.5.3 业务容器感知变化

由于 `emptyDir` 在同一 Pod 内共享，且使用原子 `rename` 操作：

```
Sidecar: 写入 docker-wrapper.sh.tmp ──► 重命名为 docker-wrapper.sh
                                               │
                                               ▼
业务容器: 下次执行 docker 命令时 ───────────────► 读取新脚本内容
```

### 3.6 状态监控

Controller 持续监控注入状态：

```bash
kubectl get behaviorstub docker-stub

NAME          PHASE    INJECTED   TOTAL   AGE
docker-stub   Running  3          3       10m
```

## 四、关键实现细节

### 4.1 原子复制

```go
func atomicCopy(srcPath, dstPath string) error {
    // 1. 创建临时文件
    tmpPath := dstPath + ".tmp"
    dstFile, err := os.Create(tmpPath)
    if err != nil {
        return err
    }

    // 2. 复制内容
    if _, err := io.Copy(dstFile, srcFile); err != nil {
        dstFile.Close()
        os.Remove(tmpPath)
        return err
    }
    dstFile.Close()

    // 3. 设置执行权限
    if err := os.Chmod(tmpPath, 0755); err != nil {
        os.Remove(tmpPath)
        return err
    }

    // 4. 原子重命名
    if err := os.Rename(tmpPath, dstPath); err != nil {
        os.Remove(tmpPath)
        return err
    }

    return nil
}
```

### 4.2 subPath 挂载

`subPath` 允许将 Volume 中的单个文件挂载到容器的指定路径，而不是挂载整个目录：

```yaml
volumeMounts:
  - name: dynastub-shared
    mountPath: /usr/bin/docker      # 容器内的目标路径
    subPath: docker-wrapper.sh      # Volume 中的文件名
```

这样 `/usr/bin/docker` 会被替换为 `emptyDir` 中的脚本，而其他命令保持不变。

### 4.3 原生 Sidecar 模式

Kubernetes 1.28+ 支持在 `initContainers` 中设置 `restartPolicy: Always`：

```yaml
initContainers:
  - name: dynastub-sidecar
    image: dynastub-sidecar:latest
    restartPolicy: Always  # 作为 sidecar 持续运行
```

特性：
- Sidecar 在业务容器之前启动
- Sidecar 保持运行，不阻塞业务容器启动
- 业务容器启动时，Sidecar 已完成脚本复制

## 五、扩展模式：Remote

当前 CRD 支持 `mode: local`，未来可扩展支持 `mode: remote`：

### 5.1 Remote 模式场景

被测组件需要连接远端 IP 执行系统命令，例如：
- SSH 到远程服务器执行命令
- 通过 API 调用远程服务

### 5.2 Remote 模式方案

```
┌─────────────────────────────────────────────────────────────────┐
│                    Remote 模式架构                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────────┐         ┌─────────────────────────────┐   │
│  │   被测组件       │────────►│  Remote Pod (动态创建)       │   │
│  │  (目标 Pod)      │  SSH/API│  - Service IP = 目标远程 IP  │   │
│  └─────────────────┘         │  - 内部执行命令打桩            │   │
│                              │    (同 Local 模式)            │   │
│                              └─────────────────────────────┘   │
│                                         ▲                      │
│                                         │ 创建和管理            │
│                              ┌──────────┴──────────┐          │
│                              │   DynaStub Operator  │          │
│                              │  (Remote 模式控制器) │          │
│                              └─────────────────────┘          │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 5.3 实现思路

当 `mode: remote` 时：

1. **Controller 创建 Remote Pod**
   - 根据 `remoteConfig.targetIP` 创建 Pod
   - 创建 Service，ClusterIP 设置为 `targetIP`
   - 在 Remote Pod 中执行 Local 模式的相同操作

2. **Remote Pod 内部**
   - 同样注入 Sidecar
   - 同样通过 emptyDir + subPath 替换命令
   - 暴露 SSH/API 端口供被测组件连接

3. **被测组件视角**
   - 连接到 "远程 IP"
   - 实际连接到 Remote Pod
   - 执行的命令已被打桩

### 5.4 可行性评估

| 方面 | 评估 |
|------|------|
| **技术可行性** | ✅ 可行。利用 K8s Service 的 ClusterIP 可以绑定指定 IP（需在 Service CIDR 范围内） |
| **网络连通性** | ✅ 被测组件通过 Service IP 访问 Remote Pod，K8s 自动负载均衡 |
| **隔离性** | ✅ 每个 Remote 目标独立一个 Pod，互不干扰 |
| **资源开销** | ⚠️ 每个远程目标需要一个 Pod，资源消耗较大 |
| **复杂性** | ⚠️ 需要管理 Pod 生命周期，处理连接状态同步 |

### 5.5 建议实现

```yaml
apiVersion: dynastub.example.com/v1
kind: BehaviorStub
metadata:
  name: remote-ssh-stub
spec:
  mode: remote
  remoteConfig:
    targetIP: 10.0.1.100      # 被测组件要连接的远程 IP
    servicePort: 22           # SSH 端口
    podTemplate:
      image: remote-stub-pod:latest
      resources:
        limits:
          cpu: "500m"
          memory: "512Mi"
  scriptVolume:
    hostPath: /opt/dynastub/remote-scripts
    mountPath: /src/scripts
  behaviors:
    - name: ssh
      targetPath: /usr/bin/ssh
      scriptPath: /src/scripts/ssh-wrapper.sh
```

**结论**：Remote 模式方案可行，建议作为后续迭代功能实现。

## 六、总结

DynaStub 通过以下机制实现命令打桩：

1. **声明式配置**：用户通过 BehaviorStub CR 声明注入规则
2. **Webhook 注入**：自动拦截 Pod 创建，注入 Sidecar 和 Volume
3. **文件替换**：通过 emptyDir + subPath 挂载替换目标命令
4. **热更新**：Sidecar 持续监听脚本变化，实时更新
5. **可扩展**：支持 Local 和 Remote 两种模式

整个流程无需修改业务镜像，无需重建 Pod，实现真正的动态命令打桩。
