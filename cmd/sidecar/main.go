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
