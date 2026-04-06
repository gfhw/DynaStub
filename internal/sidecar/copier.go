package sidecar

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ScriptCopier 复制用户脚本到目标位置
type ScriptCopier struct {
	outputDir string
}

// NewScriptCopier 创建新的脚本复制器
func NewScriptCopier(outputDir string) *ScriptCopier {
	return &ScriptCopier{
		outputDir: outputDir,
	}
}

// Copy 复制单个用户脚本
func (c *ScriptCopier) Copy(srcPath, dstName string) error {
	dstPath := filepath.Join(c.outputDir, dstName)
	return c.atomicCopy(srcPath, dstPath)
}

// atomicCopy 原子复制文件
func (c *ScriptCopier) atomicCopy(srcPath, dstPath string) error {
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

// CopyAll 复制所有脚本
func (c *ScriptCopier) CopyAll(srcDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		srcPath := filepath.Join(srcDir, entry.Name())
		if err := c.Copy(srcPath, entry.Name()); err != nil {
			return fmt.Errorf("failed to copy %s: %v", entry.Name(), err)
		}
	}

	return nil
}
