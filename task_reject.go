package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func canRejectTask(t Task) bool {
	if t.Direction != "in" || t.Kind == "text" {
		return false
	}
	switch t.Status {
	case "waiting", "receiving", "paused", "queued":
		return true
	}
	return false
}

func (n *Node) rejectTask(id string) error {
	lock := n.taskLock(id)
	lock.Lock()
	defer lock.Unlock()
	n.mu.Lock()
	t := n.cfg.Tasks[id]
	if t == nil {
		n.mu.Unlock()
		return errors.New("任务不存在")
	}
	if t.Direction == "in" && t.Kind != "text" && t.Status == "rejected" {
		n.mu.Unlock()
		return nil
	}
	if !canRejectTask(*t) {
		n.mu.Unlock()
		return errors.New("只能拒绝尚未完成接收的文件；校验保存中的文件请稍候")
	}
	t.Status, t.Manual, t.Rate, t.Error = "rejected", true, 0, ""
	copyTask := *t
	copyTask.Files = append([]Meta(nil), t.Files...)
	n.mu.Unlock()
	// Commit the terminal decision before removing any incomplete data.
	if err := n.persist(); err != nil {
		return err
	}
	cleanupErr := discardRejectedParts(copyTask)
	n.mu.Lock()
	for i := range t.Files {
		if t.Files[i].Final == "" {
			t.Files[i].Offset = 0
		}
	}
	t.Done = 0
	for _, f := range t.Files {
		t.Done += f.Offset
	}
	if cleanupErr != nil {
		t.Error = "已拒绝接收，部分临时文件未能清理"
	}
	n.mu.Unlock()
	return n.persist()
}

// Delete only this task's generated .part files. Completed files and arbitrary
// contents of a receive folder are never recursively removed.
func discardRejectedParts(t Task) error {
	var firstErr error
	for i, f := range t.Files {
		if f.Temp == "" {
			continue
		}
		dir := filepath.Dir(f.Temp)
		if filepath.Base(dir) != t.ID || filepath.Base(filepath.Dir(dir)) != ".邻传未完成" ||
			!strings.EqualFold(filepath.Clean(f.Temp), filepath.Join(dir, fmt.Sprintf("%d.part", i))) {
			firstErr = errors.New("临时文件位置异常")
			continue
		}
		if err := os.Remove(f.Temp); err != nil && !os.IsNotExist(err) {
			firstErr = err
		}
		// Remove empty task caches only; failures from nonempty directories are harmless.
		os.Remove(dir)
	}
	if t.Root != "" && (t.Folder || len(t.Files) > 1) {
		os.Remove(t.Root)
	}
	return firstErr
}
