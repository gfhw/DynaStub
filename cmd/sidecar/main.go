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
	// 设置日志输出到文件（如果指定了日志路径）
	logFile := getEnv("LOG_FILE", "/var/log/certgen/certgen.log")
	if logFile != "" {
		// 确保日志目录存在
		logDir := filepath.Dir(logFile)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			log.Printf("Warning: Failed to create log directory %s: %v", logDir, err)
		} else {
			// 打开日志文件
			f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Printf("Warning: Failed to open log file %s: %v", logFile, err)
			} else {
				defer f.Close()
				// 同时输出到文件和标准输出
				log.SetOutput(io.MultiWriter(os.Stdout, f))
				log.Printf("Logging to file: %s", logFile)
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
	log.Println("Starting DynaStub Sidecar...")

	// 获取配置
	config := loadConfig()
	log.Printf("Configuration: BehaviorStub=%s/%s, SharedDir=%s, ScriptMountPath=%s",
		config.Namespace, config.BehaviorStubName, config.SharedDir, config.ScriptMountPath)

	// 创建 K8s 动态客户端
	dynamicClient, err := createDynamicClient()
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	// 创建 Sidecar 管理器
	sidecar := NewSidecarManager(config, dynamicClient)

	// 首次同步所有脚本
	if err := sidecar.SyncAllScripts(); err != nil {
		log.Fatalf("Failed to sync scripts: %v", err)
	}

	// 标记 ready
	if err := markReady(); err != nil {
		log.Fatalf("Failed to mark ready: %v", err)
	}
	log.Println("Sidecar is ready")

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

	var caCert []byte

	// 检查证书是否已存在且有效
	exists, err := certgen.CertExistsAndValid(client, namespace, secretName)
	if err != nil {
		return fmt.Errorf("failed to check existing certificate: %v", err)
	}
	if exists {
		log.Println("Valid certificate already exists, reading CA from Secret")
		caCert, err = certgen.ReadCaCertFromSecret(client, namespace, secretName)
		if err != nil {
			return fmt.Errorf("failed to read CA certificate from existing Secret: %v", err)
		}
	} else {
		// 生成证书
		hosts := []string{
			serviceName,
			fmt.Sprintf("%s.%s", serviceName, namespace),
			fmt.Sprintf("%s.%s.svc", serviceName, namespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace),
		}

		log.Printf("Generating certificates with DNS names: %v", hosts)
		var serverCert, serverKey []byte
		caCert, serverCert, serverKey, err = certgen.GenerateCertificates(hosts)
		if err != nil {
			return fmt.Errorf("failed to generate certificates: %v", err)
		}

		// 创建或更新 Secret
		if err := certgen.CreateOrUpdateSecret(client, namespace, secretName, caCert, serverCert, serverKey); err != nil {
			return fmt.Errorf("failed to create/update secret: %v", err)
		}
		log.Printf("Secret %s/%s created/updated successfully", namespace, secretName)
	}

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
	// 从 K8s API 获取 BehaviorStub
	bs, err := s.getBehaviorStub()
	if err != nil {
		// 如果获取失败，尝试从环境变量或本地配置加载
		log.Printf("Warning: Failed to get BehaviorStub from API: %v", err)
		log.Println("Falling back to copy all scripts from source directory")
		return s.syncAllFromDirectory()
	}

	// 保存 behaviors 列表
	s.behaviors = bs.Spec.Behaviors

	// 确保目标目录存在
	if err := os.MkdirAll(s.config.SharedDir, 0755); err != nil {
		return fmt.Errorf("failed to create shared directory: %v", err)
	}

	// 根据 behaviors 列表精确复制每个脚本
	for _, behavior := range s.behaviors {
		if err := s.syncBehaviorScript(behavior); err != nil {
			log.Printf("Failed to sync script for behavior %s: %v", behavior.Name, err)
			continue
		}
	}

	return nil
}

// syncBehaviorScript 同步单个 behavior 的脚本
func (s *SidecarManager) syncBehaviorScript(behavior Behavior) error {
	// 源文件路径（在 hostPath 中）
	srcPath := filepath.Join(s.config.ScriptMountPath, behavior.ScriptPath)

	// 目标文件名（使用 targetPath 的最后一部分，或自定义映射）
	// 例如：/usr/bin/docker -> docker
	targetName := filepath.Base(behavior.TargetPath)
	dstPath := filepath.Join(s.config.SharedDir, targetName)

	log.Printf("Syncing behavior %s: %s -> %s", behavior.Name, srcPath, dstPath)

	// 检查源文件是否存在
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return fmt.Errorf("source script not found: %s", srcPath)
	}

	// 原子复制
	if err := s.atomicCopy(srcPath, dstPath); err != nil {
		return fmt.Errorf("failed to copy script: %v", err)
	}

	log.Printf("Successfully synced script for behavior %s: %s", behavior.Name, targetName)
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
	// 打开源文件
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %v", srcPath, err)
	}
	defer srcFile.Close()

	// 创建临时文件
	tmpPath := dstPath + ".tmp"
	dstFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}

	// 复制内容
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		dstFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to copy file content: %v", err)
	}

	// 关闭文件
	if err := dstFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %v", err)
	}

	// 设置执行权限
	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to set executable permission: %v", err)
	}

	// 原子重命名
	if err := os.Rename(tmpPath, dstPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename file: %v", err)
	}

	return nil
}

// WatchBehaviorStub 监听 BehaviorStub 变更（使用 K8s Watch API）
func (s *SidecarManager) WatchBehaviorStub(ctx context.Context) error {
	gvr := schema.GroupVersionResource{
		Group:    "dynastub.example.com",
		Version:  "v1",
		Resource: "behaviorstubs",
	}

	log.Printf("Starting to watch BehaviorStub: %s/%s", s.config.Namespace, s.config.BehaviorStubName)

	watchInterface, err := s.dynamicClient.Resource(gvr).
		Namespace(s.config.Namespace).
		Watch(ctx, metav1.SingleObject(metav1.ObjectMeta{
			Name:      s.config.BehaviorStubName,
			Namespace: s.config.Namespace,
		}))
	if err != nil {
		return fmt.Errorf("failed to watch BehaviorStub: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("Stopping BehaviorStub watch...")
			return nil
		case event := <-watchInterface.ResultChan():
			switch event.Type {
			case watch.Added, watch.Modified:
				log.Printf("BehaviorStub %s detected, syncing scripts...", event.Type)
				if err := s.SyncAllScripts(); err != nil {
					log.Printf("Failed to sync scripts after %s event: %v", event.Type, err)
				} else {
					log.Println("Scripts synced successfully after BehaviorStub change")
				}
			case watch.Deleted:
				log.Println("BehaviorStub deleted, cleaning up scripts...")
				s.cleanupScripts()
			case watch.Error:
				log.Printf("Watch error occurred: %v", event.Object)
			default:
				log.Printf("Unknown watch event type: %v", event.Type)
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
