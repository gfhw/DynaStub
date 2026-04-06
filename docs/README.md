DynaStub – Kubernetes 动态行为注入框架（完整方案）
一、总体架构
text
┌─────────────────────────────────────────────────────────────────────┐
│                         Kubernetes Cluster                          │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │                    DynaStub Operator                        │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌────────────────────┐  │   │
│  │  │ Controller  │  │ Webhook     │  │ BehaviorStub CRD   │  │   │
│  │  │ (watch CR)  │  │ (Pod注入)   │  │ (用户声明规则)      │  │   │
│  │  └─────────────┘  └─────────────┘  └────────────────────┘  │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                 │                                   │
│                                 │ 注入 Sidecar + Volume              │
│                                 ▼                                   │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │                      Target Pod                             │   │
│  │  ┌─────────────────────────────────────────────────────┐   │   │
│  │  │  Sidecar Container (原生 sidecar 模式)               │   │   │
│  │  │  - watch BehaviorStub CR (通过 K8s API)              │   │   │
│  │  │  - 监听用户脚本源目录 (hostPath) 文件变化             │   │   │
│  │  │  - 将脚本原子复制到 emptyDir                         │   │   │
│  │  └─────────────────────────────────────────────────────┘   │   │
│  │                                                             │   │
│  │  ┌─────────────────────────────────────────────────────┐   │   │
│  │  │  业务容器                                            │   │   │
│  │  │  - 通过 subPath 挂载 emptyDir 中的脚本 → 覆盖原命令   │   │   │
│  │  │  - 原始命令已备份为 *.original                       │   │   │
│  │  └─────────────────────────────────────────────────────┘   │   │
│  │                                                             │   │
│  │  Volume: emptyDir (shared)                                 │   │
│  │  Volume: hostPath (只读，挂载用户脚本源目录)                │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
二、核心组件职责
组件	职责
BehaviorStub CRD	用户声明注入规则：目标 Pod 选择器、要替换的命令、脚本路径、备份选项等。
Controller	监听 CR 变化，进行校验、状态更新（可选）；不直接修改 Pod。
Mutating Webhook	拦截 Pod 创建请求，根据匹配的 BehaviorStub 动态注入 Sidecar 容器、Volume 和挂载配置。
Sidecar	运行在目标 Pod 中，负责：
1. 通过 K8s API 监听 BehaviorStub CR 的变化；
2. 根据 CR 中的 scriptPath，从 hostPath 源目录读取用户脚本；
3. 将脚本原子复制到 emptyDir；
4. 支持热更新：当 CR 或脚本文件变化时，重新复制脚本。
emptyDir	存放最终生效的脚本文件，供业务容器挂载。
hostPath	节点上的目录，存放用户编写的原始包装脚本（只读挂载到 Sidecar）。
三、工作流程（从安装到运行）
3.1 安装阶段
用户准备脚本目录
在集群每个节点（或通过共享存储）上准备目录，例如 /opt/dynastub/scripts/，放入包装脚本：

bash
/opt/dynastub/scripts/docker-wrapper.sh
/opt/dynastub/scripts/kubectl-wrapper.sh
脚本需可执行，并包含透传原始命令的逻辑（原始命令会被备份为 *.original）。

部署 Operator

bash
helm install dynastub ./charts/dynastub
Helm 会创建：

BehaviorStub CRD

Operator Deployment（包含 Controller 和 Webhook）

MutatingWebhookConfiguration

必要的 RBAC 权限

3.2 注入规则定义
用户创建 BehaviorStub 对象：

yaml
apiVersion: dynastub.example.com/v1
kind: BehaviorStub
metadata:
  name: my-stub
  namespace: default
spec:
  mode: local
  localConfig:
    targetSelector:
      matchLabels:
        app: myapp
    sidecarImage: dynastub-sidecar:latest   # Sidecar 镜像
  scriptVolume:
    hostPath: /opt/dynastub/scripts         # 节点上脚本源目录
    mountPath: /src/scripts                 # Sidecar 内的挂载点
  behaviors:
    - name: docker
      targetPath: /usr/bin/docker
      scriptPath: /src/scripts/docker-wrapper.sh   # 用户脚本在 Sidecar 内的路径
      backupOriginal: true
    - name: kubectl
      targetPath: /usr/local/bin/kubectl
      scriptPath: /src/scripts/kubectl-wrapper.sh
      backupOriginal: true
kubectl apply -f behavior-stub.yaml

3.3 Pod 创建与自动注入
用户创建带标签 app: myapp 的 Pod（或 Deployment）：

yaml
apiVersion: v1
kind: Pod
metadata:
  name: myapp-pod
  labels:
    app: myapp
    dynastub-inject: enabled   # 可选，也可直接匹配标签
spec:
  containers:
  - name: app
    image: myapp:latest
Webhook 处理步骤：

拦截 Pod 创建请求，检查 Pod 标签是否匹配某个 BehaviorStub 的 targetSelector。

如果匹配，修改 Pod 定义：

添加 initContainer（用于备份原始命令）
因为需要将业务容器中的原始二进制重命名为 *.original，这需要在业务容器启动前完成。使用一个 initContainer 来完成这个任务，挂载业务容器的根文件系统（通过 volumeMounts 共享业务容器的 volume？通常需要特权，简单方式：在 Webhook 中修改业务容器的 command，在启动前执行备份脚本？更稳妥：使用一个 initContainer 挂载业务容器的 rootfs（需要 volumeMount 的 subPath 或 mountPropagation），但复杂度高。替代方案：由 Sidecar 在 emptyDir 中放置一个备份脚本，业务容器启动时执行该脚本来备份原始二进制。但业务容器的启动命令无法被轻易修改。
更简单可靠：Webhook 不负责备份，而是在业务容器的 postStart 生命周期钩子中执行备份，或者依赖用户镜像中已经存在备份。为了简化，可以要求用户在包装脚本中动态检查并备份（首次运行时备份）。
实际上，备份原始命令的最佳时机：在 Sidecar 首次复制脚本到 emptyDir 后，业务容器第一次执行该命令时，包装脚本检查是否存在 target.original，若不存在则复制 target 到 target.original。这种方式无需特权，且兼容性好。

添加 emptyDir 卷（名称：dynastub-shared）

添加 hostPath 卷（名称：dynastub-scripts，只读，指向 scriptVolume.hostPath）

添加 Sidecar 容器（使用原生 sidecar 特性，确保在业务容器之前启动）
在 Kubernetes 1.28+ 中，可以在 initContainers 中定义容器并设置 restartPolicy: Always，这样它会作为 sidecar 在业务容器之前启动并持续运行。

修改业务容器：

挂载 emptyDir 卷到某个临时路径（如 /dynastub/bin），并通过 subPath 将每个目标命令的脚本文件挂载到原命令路径。例如：

yaml
volumeMounts:
- name: dynastub-shared
  mountPath: /usr/bin/docker
  subPath: docker-wrapper.sh
由于 subPath 要求目标文件在挂载前已存在，所以必须保证 Sidecar 在业务容器启动前已将脚本写入 emptyDir。这正是使用原生 sidecar 的原因：Sidecar 作为 initContainer 启动并持续运行，业务容器会等待 Sidecar 就绪（kubelet 会按顺序启动 initContainers，sidecar 会保持运行，然后才启动普通容器）。

Webhook 返回修改后的 Pod 定义。

3.4 Pod 启动顺序与脚本就绪
kubelet 创建 Pod 的 volume（emptyDir 初始为空，hostPath 已存在）。

启动 Sidecar 容器（作为 initContainer 类型且 restartPolicy: Always）。

Sidecar 启动后，立即通过 K8s API 获取当前 Namespace 中的 BehaviorStub 列表（或通过环境变量指定 CR 名称）。

根据匹配的 CR（通过 Pod 标签关联），读取 behaviors 配置，获取需要处理的命令列表和对应的 scriptPath。

从 hostPath 挂载的源目录（/src/scripts）中读取用户脚本内容。

将脚本内容原子写入 emptyDir 卷的对应文件（如 docker-wrapper.sh），并设置可执行权限。

启动文件监听（inotify）和 CR 监听，等待后续变更。

当 Sidecar 完成首次写入后，kubelet 启动业务容器。

此时 emptyDir 中已存在 docker-wrapper.sh 等文件，业务容器的挂载成功。

业务容器内的 /usr/bin/docker 指向包装脚本。

业务容器运行过程中，执行 docker 命令时，实际执行包装脚本。

3.5 热更新机制
场景一：用户修改了 BehaviorStub CR（例如添加新的命令、更改脚本路径）

Sidecar 通过 watch K8s API 感知到 CR 变化。

Sidecar 重新读取 CR，调整需要监控的脚本列表。

对于新增/修改的脚本，重新从 hostPath 读取并写入 emptyDir。

场景二：用户修改了 hostPath 中的脚本文件（例如 docker-wrapper.sh 内容变化）

Sidecar 通过 inotify 监控 /src/scripts 目录，感知文件变更事件。

Sidecar 将新内容原子写入 emptyDir 对应的文件（先写临时文件，再 rename 覆盖）。

由于 rename 在同一文件系统内是原子的，业务容器下次打开 /usr/bin/docker 时会看到新脚本内容（因为路径解析重新获取 dentry）。

关键点：业务容器通过 subPath 挂载 emptyDir 中的文件，而 emptyDir 是目录，Sidecar 通过 rename 更新文件会改变目录项，因此业务容器能感知到变化，实现热更新。

3.6 原始命令的备份与透传
用户在编写包装脚本时，需要自行处理原始命令的调用。建议脚本模板如下：

bash
#!/bin/sh
# 备份原始命令（首次运行时）
ORIGINAL="/usr/bin/docker.original"
if [ ! -f "$ORIGINAL" ]; then
    cp /usr/bin/docker "$ORIGINAL"
fi

# 记录日志
echo "$(date): docker $@" >> /tmp/dynastub.log

# 执行原始命令
exec "$ORIGINAL" "$@"
这样无需依赖外部备份机制，完全由脚本自身完成，兼容性好。

四、Sidecar 实现细节
4.1 Sidecar 需要的信息
为了知道要处理哪些命令以及对应的脚本路径，Sidecar 需要获取当前 Pod 匹配的 BehaviorStub 配置。有几种方式：

通过环境变量传递：Webhook 注入 Sidecar 时，设置环境变量 BEHAVIOR_STUB_NAME=my-stub，Sidecar 根据名称去 K8s API 查询 CR。

通过标签选择器：Sidecar 启动后，获取自身 Pod 的标签，然后查询匹配的 BehaviorStub。

监听所有 CR：Sidecar 监听同 Namespace 下所有 BehaviorStub 对象，并检查其 targetSelector 是否匹配当前 Pod 的标签。

推荐方式 1 + 3：Webhook 注入时将匹配的 CR 名称列表通过环境变量传递给 Sidecar，Sidecar 只 watch 这些 CR，减少 API 开销。

4.2 Sidecar 的核心逻辑（伪代码）
python
def main():
    # 从环境变量获取 CR 名称列表
    cr_names = os.getenv("BEHAVIOR_STUB_NAMES", "").split(",")
    # 初始化 K8s client
    client = k8s_client()
    
    # 启动 goroutine 监听 CR 变化
    for name in cr_names:
        watch_cr(name, on_cr_changed)
    
    # 启动文件监听 (inotify) 监控 /src/scripts
    watch_files("/src/scripts", on_file_changed)
    
    # 首次同步所有脚本
    sync_all_scripts()
    
    # 保持运行
    select {}


def sync_all_scripts():
    for each behavior in current_cr.spec.behaviors:
        src = behavior.scriptPath   # 例如 /src/scripts/docker-wrapper.sh
        dst = "/shared/" + os.path.basename(src)   # emptyDir 挂载点
        atomic_copy(src, dst)
        os.chmod(dst, 0o755)

def on_cr_changed(new_cr):
    update current_cr
    sync_all_scripts()

def on_file_changed(file_path):
    # 查找对应的 behavior
    for b in current_cr.spec.behaviors:
        if b.scriptPath == file_path:
            atomic_copy(file_path, "/shared/"+os.path.basename(file_path))
            break
4.3 原子复制实现
python
def atomic_copy(src, dst):
    tmp = dst + ".tmp"
    shutil.copy2(src, tmp)
    os.rename(tmp, dst)   # rename 在同一文件系统内是原子的
五、启动顺序保证（原生 Sidecar）
Kubernetes 1.28+ 引入了对 initContainers 中 restartPolicy: Always 的支持，使得 initContainer 可以作为 sidecar 在业务容器之前启动并持续运行。

在 Webhook 注入时，添加如下配置：

yaml
initContainers:
- name: dynastub-sidecar
  image: dynastub-sidecar:latest
  restartPolicy: Always   # 关键：使其成为 sidecar
  volumeMounts:
  - name: dynastub-scripts
    mountPath: /src/scripts
  - name: dynastub-shared
    mountPath: /shared
  env:
  - name: BEHAVIOR_STUB_NAMES
    value: "my-stub"
这样，kubelet 会先启动 sidecar，等待它运行后（不需要退出），再启动业务容器。业务容器启动时，shared 目录中已经有脚本文件，挂载成功。

六、清理与回滚
当删除 BehaviorStub CR 时，Controller 可以发送一个清理事件，但无法直接修改已运行的 Pod。

推荐做法：用户重建受影响的 Pod（通过滚动更新），新 Pod 不再被注入（因为 CR 已删除）。

如果需要动态恢复，Sidecar 可以监听 CR 删除事件，并删除 emptyDir 中的脚本文件，但业务容器的挂载点会变为空文件，命令执行失败。更好的方式是 Sidecar 将原始备份（如果存在）复制回原路径，但这需要额外逻辑。通常不实现动态恢复，而是依赖 Pod 重建。

七、方案优缺点总结
优点
真正的动态注入：脚本更新秒级生效，无需重建 Pod。

用户完全控制脚本逻辑：无 DSL 学习成本，可写任意复杂逻辑。

架构清晰：组件职责分明，Sidecar 只负责同步脚本，不侵入业务。

利用 K8s 原生特性：emptyDir + subPath + 原生 sidecar，可靠性高。

缺点
每个注入的 Pod 多一个 Sidecar 容器：轻微资源开销。

依赖 Kubernetes 1.28+ 才能使用原生 sidecar 特性（否则需要其他顺序保障手段，如 initContainer + 等待脚本就绪的脚本）。

用户需要自行编写 Shell 脚本，并正确处理参数传递、信号、退出码等。

八、后续可扩展方向
支持从 ConfigMap 读取脚本：无需节点 hostPath，脚本版本化。

支持多脚本源：Git、OBS、HTTP 等。

提供脚本模板生成器：dynastub scaffold 帮助用户生成正确的包装脚本。

集成 Prometheus metrics：Sidecar 暴露脚本更新次数、延迟等指标。