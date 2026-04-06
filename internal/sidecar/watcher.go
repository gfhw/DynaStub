package sidecar

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"
)

// FileWatcher 文件监听器
type FileWatcher struct {
	srcDir    string
	copier    *ScriptCopier
	interval  time.Duration
	fileHashes map[string]string
}

// NewFileWatcher 创建新的文件监听器
func NewFileWatcher(srcDir, outputDir string, interval time.Duration) *FileWatcher {
	return &FileWatcher{
		srcDir:     srcDir,
		copier:     NewScriptCopier(outputDir),
		interval:   interval,
		fileHashes: make(map[string]string),
	}
}

// Start 启动文件监听
func (w *FileWatcher) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// 首次同步
	if err := w.sync(); err != nil {
		log.Printf("Initial sync failed: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.sync(); err != nil {
				log.Printf("Sync failed: %v", err)
			}
		}
	}
}

// sync 同步文件
func (w *FileWatcher) sync() error {
	entries, err := os.ReadDir(w.srcDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		srcPath := filepath.Join(w.srcDir, entry.Name())
		info, err := os.Stat(srcPath)
		if err != nil {
			continue
		}

		// 检查文件是否变化
		currentHash := w.getFileHash(srcPath, info.ModTime())
		if lastHash, exists := w.fileHashes[entry.Name()]; !exists || lastHash != currentHash {
			// 文件变化，复制
			if err := w.copier.Copy(srcPath, entry.Name()); err != nil {
				log.Printf("Failed to copy %s: %v", entry.Name(), err)
				continue
			}
			w.fileHashes[entry.Name()] = currentHash
			log.Printf("Updated script: %s", entry.Name())
		}
	}

	return nil
}

// getFileHash 获取文件哈希（简化版：使用修改时间）
func (w *FileWatcher) getFileHash(path string, modTime time.Time) string {
	return modTime.String()
}
