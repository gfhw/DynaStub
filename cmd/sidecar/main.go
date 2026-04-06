package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	readyMarkerFile = "/tmp/dynastub-ready"
)

func main() {
	log.Println("Starting DynaStub Sidecar...")

	// 获取配置
	config := loadConfig()
	log.Printf("Configuration: BehaviorStub=%s/%s, SharedDir=%s, ScriptMountPath=%s",
		config.Namespace, config.BehaviorStubName, config.SharedDir, config.ScriptMountPath)

	// 创建 K8s 客户端
	k8sClient, err := createK8sClient()
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	// 创建 Sidecar 管理器
	sidecar := NewSidecarManager(config, k8sClient)

	// 首次同步所有脚本
	if err := sidecar.SyncAllScripts(); err != nil {
		log.Fatalf("Failed to sync scripts: %v", err)
	}

	// 标记 ready
	if err := markReady(); err != nil {
		log.Fatalf("Failed to mark ready: %v", err)
	}
	log.Println("Sidecar is ready")

	// 启动文件监听
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sidecar.WatchScripts(ctx)

	// 等待信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	log.Println("Shutting down...")
	cancel()

	// 清理 ready marker
	os.Remove(readyMarkerFile)
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

// createK8sClient 创建 Kubernetes 客户端
func createK8sClient() (*kubernetes.Clientset, error) {
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

	return kubernetes.NewForConfig(config)
}

// markReady 标记 Sidecar 已就绪
func markReady() error {
	return os.WriteFile(readyMarkerFile, []byte("ready"), 0644)
}

// SidecarManager Sidecar 管理器
type SidecarManager struct {
	config    *Config
	k8sClient *kubernetes.Clientset
}

// NewSidecarManager 创建新的 Sidecar 管理器
func NewSidecarManager(config *Config, k8sClient *kubernetes.Clientset) *SidecarManager {
	return &SidecarManager{
		config:    config,
		k8sClient: k8sClient,
	}
}

// SyncAllScripts 同步所有脚本
func (s *SidecarManager) SyncAllScripts() error {
	// 从 K8s API 获取 BehaviorStub
	// 注意：这里简化处理，实际应该从 API Server 获取
	// 为了简化，我们假设脚本已经在 hostPath 中

	// 获取脚本源目录
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

		if err := s.copyScript(srcPath, dstPath); err != nil {
			log.Printf("Failed to copy script %s: %v", entry.Name(), err)
			continue
		}

		log.Printf("Copied script: %s", entry.Name())
	}

	return nil
}

// copyScript 复制脚本（原子操作）
func (s *SidecarManager) copyScript(srcPath, dstPath string) error {
	// 打开源文件
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %v", err)
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

// WatchScripts 监听脚本变化
func (s *SidecarManager) WatchScripts(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

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
