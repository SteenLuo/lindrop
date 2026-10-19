package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (n *Node) exportTaskPaths(id string) ([]string, error) {
	n.mu.Lock()
	p := n.cfg.Tasks[id]
	if p == nil {
		n.mu.Unlock()
		return nil, errors.New("找不到此文件消息")
	}
	t := *p
	t.Files = append([]Meta(nil), p.Files...)
	n.mu.Unlock()
	if t.Kind == "text" {
		return nil, errors.New("文字消息请使用复制文字")
	}
	if t.Direction == "in" && t.Status != "done" {
		return nil, errors.New("请先完整接收文件，再拖出")
	}
	if len(t.Files) == 0 {
		return nil, errors.New("此消息没有可拖出的文件")
	}
	paths := []string{}
	seen := map[string]bool{}
	for _, f := range t.Files {
		path := f.Source
		if t.Direction == "in" {
			path = f.Final
		}
		if path == "" {
			return nil, errors.New("文件尚未保存到本机")
		}
		abs, e := filepath.Abs(path)
		if e != nil {
			return nil, e
		}
		if st, e := os.Stat(abs); e != nil || !st.Mode().IsRegular() {
			return nil, fmt.Errorf("原文件已移动或无法读取：%s", filepath.Base(path))
		}
		if !seen[strings.ToLower(abs)] {
			paths = append(paths, abs)
			seen[strings.ToLower(abs)] = true
		}
	}
	// Only received tasks with their own dedicated directory may export Root.
	if t.Direction == "in" && (t.Folder || len(t.Files) > 1) && t.Root != "" {
		root, e := filepath.Abs(t.Root)
		if e != nil {
			return nil, e
		}
		for _, path := range paths {
			rel, e := filepath.Rel(root, path)
			if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil, errors.New("文件位置已改变，请打开所在文件夹")
			}
		}
		return []string{root}, nil
	}
	if t.Direction == "out" && t.Folder {
		root := paths[0]
		for range strings.Split(filepath.FromSlash(t.Files[0].Rel), string(filepath.Separator)) {
			root = filepath.Dir(root)
		}
		for _, f := range t.Files {
			source, e := filepath.Abs(f.Source)
			if e != nil || !strings.EqualFold(filepath.Clean(filepath.Join(root, filepath.FromSlash(f.Rel))), source) {
				return nil, errors.New("原文件夹位置已改变")
			}
		}
		return []string{root}, nil
	}
	return paths, nil
}
