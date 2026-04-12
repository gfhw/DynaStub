package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"httpteststub.example.com/internal/certgen"
)

const (
	readyMarkerFile = "/tmp/dynastub-ready"
)

func main() {
	log.Println("[Sidecar] ========================================")
	log.Println("[Sidecar] Starting DynaStub Sidecar")
	log.Println("[Sidecar] ========================================")

	// 设置日志输出到文件（如果指定了日志路径）
	logFile := getEnv("LOG_FILE", "/var/log/certgen/certgen.log")
	if logFile != "" {
		// 确保日志目录存在
		logDir := filepath.Dir(logFile)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			log.Printf("[Sidecar] Warning: Failed to create log directory %s: %v", logDir, err)
		} else {
			// 打开日志文件
			f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Printf("[Sidecar] Warning: Failed to open log file %s: %v", logFile, err)
			} else {
				defer f.Close()
				// 同时输出到文件和标准输出
				log.SetOutput(io.MultiWriter(os.Stdout, f))
				log.Printf("[Sidecar] Logging to file: %s", logFile)
			}
		}
	}

	// 检查是否为证书生成模式
	certGenMode := getEnv("CERTGEN_MODE", "false")
	if certGenMode == "true" {
		log.Println("========================================")
		log.Println("Running in certificate generation mode...")
		log.Println("========================================")
		if err := runCertGenMode(); err != nil {
			log.Printf("ERROR: Certificate generation failed: %v", err)
			log.Println("========================================")
			os.Exit(1)
		}
		log.Println("========================================")
		log.Println("Certificate generation completed successfully")
		log.Println("========================================")
		return
	}

	// 正常运行模式
	log.Println("[Sidecar] Running in normal sidecar mode")

	// 获取配置
	config := loadConfig()
	log.Printf("[Sidecar] Configuration loaded:")
	log.Printf("[Sidecar]   - BehaviorStub: %s/%s", config.Namespace, config.BehaviorStubName)
	log.Printf("[Sidecar]   - SharedDir: %s", config.SharedDir)
	log.Printf("[Sidecar]   - ScriptMountPath: %s", config.ScriptMountPath)

	// 检查目录
	log.Printf("[Sidecar] Checking directories...")
	if _, err := os.Stat(config.ScriptMountPath); err != nil {
		log.Printf("[Sidecar] WARNING: Script mount path %s does not exist or is not accessible: %v", config.ScriptMountPath, err)
	} else {
		log.Printf("[Sidecar] Script mount path %s is accessible", config.ScriptMountPath)
	}

	// 创建 K8s 动态客户端
	log.Println("[Sidecar] Creating Kubernetes dynamic client...")
	dynamicClient, err := createDynamicClient()
	if err != nil {
		log.Fatalf("[Sidecar] FATAL: Failed to create Kubernetes client: %v", err)
	}
	log.Println("[Sidecar] Kubernetes dynamic client created successfully")

	// 创建 Sidecar 管理器
	log.Println("[Sidecar] Creating Sidecar manager...")
	sidecar := NewSidecarManager(config, dynamicClient)

	// 首次同步所有脚本
	log.Println("[Sidecar] Starting initial script sync...")
	if err := sidecar.SyncAllScripts(); err != nil {
		log.Fatalf("[Sidecar] FATAL: Failed to sync scripts: %v", err)
	}
	log.Println("[Sidecar] Initial script sync completed successfully")

	// 标记 ready
	log.Println("[Sidecar] Marking sidecar as ready...")
	if err := markReady(); err != nil {
		log.Fatalf("[Sidecar] FATAL: Failed to mark ready: %v", err)
	}
	log.Println("[Sidecar] ========================================")
	log.Println("[Sidecar] Sidecar is READY")
	log.Println("[Sidecar] ========================================")

	// 启动监听
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动 Watch API 监听（主监听方式）
	go func() {
		if err := sidecar.WatchBehaviorStub(ctx); err != nil {
			log.Printf("WatchBehaviorStub failed: %v", err)
			log.Println("Falling back to polling mode...")
			// 如果 Watch 失败，回退到轮询模式
			sidecar.WatchScripts(ctx)
		}
	}()

	// 等待信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	log.Println("Shutting down...")
	cancel()

	// 清理 ready marker
	os.Remove(readyMarkerFile)
}

// runCertGenMode 运行证书生成模式
func runCertGenMode() error {
	// 读取证书生成配置
	namespace := getEnv("NAMESPACE", "default")
	secretName := getEnv("SECRET_NAME", "dynastub-webhook-tls")
	webhookName := getEnv("WEBHOOK_NAME", "dynastub-k8s-http-fake-operator-webhook")
	serviceName := getEnv("SERVICE_NAME", "k8s-http-fake-operator-webhook")

	log.Printf("Certificate generation config: namespace=%s, secretName=%s, webhookName=%s, serviceName=%s",
		namespace, secretName, webhookName, serviceName)

	// 创建 K8s 客户端
	config, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return fmt.Errorf("failed to create Kubernetes config: %v", err)
		}
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	// 每次部署/升级都重新生成证书，确保使用最新配置
	hosts := []string{
		serviceName,
		fmt.Sprintf("%s.%s", serviceName, namespace),
		fmt.Sprintf("%s.%s.svc", serviceName, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace),
	}

	log.Printf("Generating certificates with DNS names: %v", hosts)
	caCert, serverCert, serverKey, err := certgen.GenerateCertificates(hosts)
	if err != nil {
		return fmt.Errorf("failed to generate certificates: %v", err)
	}

	// 创建或更新 Secret
	if err := certgen.CreateOrUpdateSecret(client, namespace, secretName, caCert, serverCert, serverKey); err != nil {
		return fmt.Errorf("failed to create/update secret: %v", err)
	}
	log.Printf("Secret %s/%s created/updated successfully", namespace, secretName)

	// 创建或更新 MutatingWebhookConfiguration（无论证书是否存在，都要确保 Webhook 配置存在）
	log.Printf("Creating/updating MutatingWebhookConfiguration: %s", webhookName)
	if err := certgen.CreateOrUpdateWebhookConfiguration(client, webhookName, namespace, serviceName, caCert); err != nil {
		return fmt.Errorf("failed to create/update webhook configuration: %v", err)
	}
	log.Printf("MutatingWebhookConfiguration %s created/updated successfully", webhookName)

	return nil
}

// Config 配置结构
type Config struct {
	BehaviorStubName string
	Namespace        string
	SharedDir        string
	ScriptMountPath  string
}

// loadConfig 加载配置
func loadConfig() *Config {
	return &Config{
		BehaviorStubName: getEnv("BEHAVIOR_STUB_NAME", ""),
		Namespace:        getEnv("BEHAVIOR_STUB_NAMESPACE", "default"),
		SharedDir:        getEnv("SHARED_DIR", "/shared"),
		ScriptMountPath:  getEnv("SCRIPT_MOUNT_PATH", "/src/scripts"),
	}
}

// getEnv 获取环境变量，如果不存在则返回默认值
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// createDynamicClient 创建 Kubernetes 动态客户端
func createDynamicClient() (dynamic.Interface, error) {
	var config *rest.Config
	var err error

	// 尝试使用 in-cluster 配置
	config, err = rest.InClusterConfig()
	if err != nil {
		// 尝试使用 kubeconfig
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create Kubernetes config: %v", err)
		}
	}

	return dynamic.NewForConfig(config)
}

// markReady 标记 Sidecar 已就绪
func markReady() error {
	return os.WriteFile(readyMarkerFile, []byte("ready"), 0644)
}

// Behavior 定义单个行为注入配置
type Behavior struct {
	Name          string `json:"name"`
	TargetPath    string `json:"targetPath"`
	ScriptPath    string `json:"scriptPath"`
	EnableLogging bool   `json:"enableLogging,omitempty"`
	LogPath       string `json:"logPath,omitempty"`
}

// SidecarManager Sidecar 管理器
type SidecarManager struct {
	config        *Config
	dynamicClient dynamic.Interface
	behaviors     []Behavior
}

// NewSidecarManager 创建新的 Sidecar 管理器
func NewSidecarManager(config *Config, dynamicClient dynamic.Interface) *SidecarManager {
	return &SidecarManager{
		config:        config,
		dynamicClient: dynamicClient,
		behaviors:     make([]Behavior, 0),
	}
}

// getBehaviorStub 从 K8s API 获取 BehaviorStub
func (s *SidecarManager) getBehaviorStub() (*BehaviorStub, error) {
	gvr := schema.GroupVersionResource{
		Group:    "dynastub.example.com",
		Version:  "v1",
		Resource: "behaviorstubs",
	}

	unstructuredObj, err := s.dynamicClient.Resource(gvr).
		Namespace(s.config.Namespace).
		Get(context.Background(), s.config.BehaviorStubName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get BehaviorStub: %v", err)
	}

	// 转换为 JSON
	data, err := unstructuredObj.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal BehaviorStub: %v", err)
	}

	// 解析为结构体
	var bs BehaviorStub
	if err := json.Unmarshal(data, &bs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal BehaviorStub: %v", err)
	}

	return &bs, nil
}

// BehaviorStub 定义
type BehaviorStub struct {
	Spec struct {
		Behaviors []Behavior `json:"behaviors"`
	} `json:"spec"`
}

// SyncAllScripts 同步所有脚本
func (s *SidecarManager) SyncAllScripts() error {
	log.Println("[Sidecar] ========================================")
	log.Println("[Sidecar] Starting SyncAllScripts")
	log.Println("[Sidecar] ========================================")

	// 从 K8s API 获取 BehaviorStub
	log.Println("[Sidecar] Fetching BehaviorStub from Kubernetes API...")
	bs, err := s.getBehaviorStub()
	if err != nil {
		// 如果获取失败，尝试从环境变量或本地配置加载
		log.Printf("[Sidecar] WARNING: Failed to get BehaviorStub from API: %v", err)
		log.Println("[Sidecar] Falling back to copy all scripts from source directory")
		return s.syncAllFromDirectory()
	}

	log.Printf("[Sidecar] BehaviorStub fetched successfully")

	// 保存 behaviors 列表
	s.behaviors = bs.Spec.Behaviors
	log.Printf("[Sidecar] Number of behaviors configured: %d", len(s.behaviors))
	for i, behavior := range s.behaviors {
		log.Printf("[Sidecar]   - Behavior %d: name=%s, targetPath=%s, scriptPath=%s",
			i+1, behavior.Name, behavior.TargetPath, behavior.ScriptPath)
	}

	// 确保目标目录存在
	log.Printf("[Sidecar] Ensuring shared directory exists: %s", s.config.SharedDir)
	if err := os.MkdirAll(s.config.SharedDir, 0755); err != nil {
		log.Printf("[Sidecar] ERROR: Failed to create shared directory: %v", err)
		return fmt.Errorf("failed to create shared directory: %v", err)
	}
	log.Println("[Sidecar] Shared directory ready")

	// 根据 behaviors 列表精确复制每个脚本
	log.Println("[Sidecar] Starting to sync individual behavior scripts...")
	successCount := 0
	for _, behavior := range s.behaviors {
		if err := s.syncBehaviorScript(behavior); err != nil {
			log.Printf("[Sidecar] ERROR: Failed to sync script for behavior %s: %v", behavior.Name, err)
			continue
		}
		successCount++
	}

	log.Printf("[Sidecar] Script sync completed: %d/%d successful", successCount, len(s.behaviors))
	log.Println("[Sidecar] ========================================")
	log.Println("[Sidecar] SyncAllScripts completed")
	log.Println("[Sidecar] ========================================")

	return nil
}

// syncBehaviorScript 同步单个 behavior 的脚本
func (s *SidecarManager) syncBehaviorScript(behavior Behavior) error {
	log.Printf("[Sidecar] Syncing behavior: %s", behavior.Name)

	// 源文件路径（在 hostPath 中）
	srcPath := filepath.Join(s.config.ScriptMountPath, behavior.ScriptPath)
	log.Printf("[Sidecar]   - Source script path: %s", srcPath)

	// 目标文件名：使用 targetPath 的最后一部分作为命令名
	// 例如：/bin/ls -> ls（去掉任何扩展名，确保就是命令名）
	targetCommand := filepath.Base(behavior.TargetPath)
	// 去掉扩展名，确保文件名就是纯命令名
	targetCommand = targetCommand[:len(targetCommand)-len(filepath.Ext(targetCommand))]
	dstPath := filepath.Join(s.config.SharedDir, targetCommand)

	log.Printf("[Sidecar]   - Target command: %s", targetCommand)
	log.Printf("[Sidecar]   - Destination path: %s", dstPath)

	// 检查源文件是否存在
	log.Printf("[Sidecar]   - Checking if source script exists...")
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		log.Printf("[Sidecar]   - ERROR: Source script not found: %s", srcPath)
		return fmt.Errorf("source script not found: %s", srcPath)
	}
	log.Printf("[Sidecar]   - Source script found")

	// 原子复制
	log.Printf("[Sidecar]   - Starting atomic copy...")
	if err := s.atomicCopy(srcPath, dstPath); err != nil {
		log.Printf("[Sidecar]   - ERROR: Failed to copy script: %v", err)
		return fmt.Errorf("failed to copy script: %v", err)
	}

	log.Printf("[Sidecar] Successfully synced script for behavior %s: %s -> %s",
		behavior.Name, srcPath, dstPath)

	// 验证目标文件
	log.Printf("[Sidecar]   - Verifying destination file...")
	if info, err := os.Stat(dstPath); err != nil {
		log.Printf("[Sidecar]   - WARNING: Failed to verify destination file: %v", err)
	} else {
		log.Printf("[Sidecar]   - Destination file verified: size=%d bytes, mode=%v",
			info.Size(), info.Mode())
	}

	return nil
}

// syncAllFromDirectory 从源目录复制所有脚本（降级方案）
func (s *SidecarManager) syncAllFromDirectory() error {
	srcDir := s.config.ScriptMountPath
	dstDir := s.config.SharedDir

	// 确保目标目录存在
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("failed to create shared directory: %v", err)
	}

	// 读取源目录中的所有脚本
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())

		if err := s.atomicCopy(srcPath, dstPath); err != nil {
			log.Printf("Failed to copy script %s: %v", entry.Name(), err)
			continue
		}

		log.Printf("Copied script: %s", entry.Name())
	}

	return nil
}

// atomicCopy 原子复制文件
func (s *SidecarManager) atomicCopy(srcPath, dstPath string) error {
	log.Printf("[Sidecar] [atomicCopy] Starting atomic copy: %s -> %s", srcPath, dstPath)

	// 打开源文件
	log.Printf("[Sidecar] [atomicCopy] Opening source file...")
	srcFile, err := os.Open(srcPath)
	if err != nil {
		log.Printf("[Sidecar] [atomicCopy] ERROR: Failed to open source file: %v", err)
		return fmt.Errorf("failed to open source file %s: %v", srcPath, err)
	}
	defer srcFile.Close()
	log.Printf("[Sidecar] [atomicCopy] Source file opened successfully")

	// 创建临时文件
	tmpPath := dstPath + ".tmp"
	log.Printf("[Sidecar] [atomicCopy] Creating temp file: %s", tmpPath)
	dstFile, err := os.Create(tmpPath)
	if err != nil {
		log.Printf("[Sidecar] [atomicCopy] ERROR: Failed to create temp file: %v", err)
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	log.Printf("[Sidecar] [atomicCopy] Temp file created successfully")

	// 复制内容
	log.Printf("[Sidecar] [atomicCopy] Copying file content...")
	bytesCopied, err := io.Copy(dstFile, srcFile)
	if err != nil {
		dstFile.Close()
		os.Remove(tmpPath)
		log.Printf("[Sidecar] [atomicCopy] ERROR: Failed to copy file content: %v", err)
		return fmt.Errorf("failed to copy file content: %v", err)
	}
	log.Printf("[Sidecar] [atomicCopy] File content copied: %d bytes", bytesCopied)

	// 关闭文件
	log.Printf("[Sidecar] [atomicCopy] Closing temp file...")
	if err := dstFile.Close(); err != nil {
		os.Remove(tmpPath)
		log.Printf("[Sidecar] [atomicCopy] ERROR: Failed to close temp file: %v", err)
		return fmt.Errorf("failed to close temp file: %v", err)
	}
	log.Printf("[Sidecar] [atomicCopy] Temp file closed successfully")

	// 设置执行权限
	log.Printf("[Sidecar] [atomicCopy] Setting executable permission (0755)...")
	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		log.Printf("[Sidecar] [atomicCopy] ERROR: Failed to set executable permission: %v", err)
		return fmt.Errorf("failed to set executable permission: %v", err)
	}
	log.Printf("[Sidecar] [atomicCopy] Executable permission set successfully")

	// 原子重命名
	log.Printf("[Sidecar] [atomicCopy] Performing atomic rename: %s -> %s", tmpPath, dstPath)
	if err := os.Rename(tmpPath, dstPath); err != nil {
		os.Remove(tmpPath)
		log.Printf("[Sidecar] [atomicCopy] ERROR: Failed to rename file: %v", err)
		return fmt.Errorf("failed to rename file: %v", err)
	}
	log.Printf("[Sidecar] [atomicCopy] Atomic rename completed successfully")

	log.Printf("[Sidecar] [atomicCopy] Atomic copy finished successfully: %s -> %s", srcPath, dstPath)
	return nil
}

// WatchBehaviorStub 监听 BehaviorStub 变更（使用 K8s Watch API）
func (s *SidecarManager) WatchBehaviorStub(ctx context.Context) error {
	gvr := schema.GroupVersionResource{
		Group:    "dynastub.example.com",
		Version:  "v1",
		Resource: "behaviorstubs",
	}

	log.Printf("[Sidecar] Starting to watch BehaviorStub: %s/%s", s.config.Namespace, s.config.BehaviorStubName)

	watchInterface, err := s.dynamicClient.Resource(gvr).
		Namespace(s.config.Namespace).
		Watch(ctx, metav1.SingleObject(metav1.ObjectMeta{
			Name:      s.config.BehaviorStubName,
			Namespace: s.config.Namespace,
		}))
	if err != nil {
		log.Printf("[Sidecar] ERROR: Failed to watch BehaviorStub: %v", err)
		return fmt.Errorf("failed to watch BehaviorStub: %v", err)
	}
	log.Println("[Sidecar] BehaviorStub watch started successfully")

	for {
		select {
		case <-ctx.Done():
			log.Println("[Sidecar] Context cancelled, stopping BehaviorStub watch...")
			return nil
		case event := <-watchInterface.ResultChan():
			log.Printf("[Sidecar] Received BehaviorStub event: type=%s", event.Type)
			switch event.Type {
			case watch.Added, watch.Modified:
				log.Printf("[Sidecar] BehaviorStub %s detected, syncing scripts...", event.Type)
				if err := s.SyncAllScripts(); err != nil {
					log.Printf("[Sidecar] ERROR: Failed to sync scripts after %s event: %v", event.Type, err)
				} else {
					log.Println("[Sidecar] Scripts synced successfully after BehaviorStub change")
				}
			case watch.Deleted:
				log.Println("[Sidecar] BehaviorStub deleted, cleaning up scripts...")
				s.cleanupScripts()
			case watch.Error:
				log.Printf("[Sidecar] ERROR: Watch error occurred: %v", event.Object)
			default:
				log.Printf("[Sidecar] WARNING: Unknown watch event type: %v", event.Type)
			}
		}
	}
}

// cleanupScripts 清理已复制的脚本
func (s *SidecarManager) cleanupScripts() {
	if err := os.RemoveAll(s.config.SharedDir); err != nil {
		log.Printf("Failed to cleanup scripts directory: %v", err)
	} else {
		log.Printf("Cleaned up scripts directory: %s", s.config.SharedDir)
	}
}

// WatchScripts 监听脚本变化（保持向后兼容，使用慢速轮询作为备份）
func (s *SidecarManager) WatchScripts(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	log.Println("Starting fallback polling watcher (30s interval)...")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.SyncAllScripts(); err != nil {
				log.Printf("Failed to sync scripts: %v", err)
			}
		}
	}
}
