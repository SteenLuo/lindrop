package main

import (
	"errors"
	"os"
	"path/filepath"
)

// Locate the actual saved file, including a renamed collision. Changing the
// default receive folder later must not redirect a historical file message.
func (n *Node) taskFileLocation(id string) (folder, selected string, err error) {
	n.mu.Lock()
	p := n.cfg.Tasks[id]
	if p == nil {
		n.mu.Unlock()
		return "", "", errors.New("找不到此文件消息")
	}
	t := *p
	t.Files = append([]Meta(nil), p.Files...)
	n.mu.Unlock()
	if t.Kind == "text" || t.Direction != "in" && t.Direction != "out" {
		return "", "", errors.New("请选择本机的文件消息")
	}
	if t.Direction == "in" && t.Status != "done" {
		return "", "", errors.New("请先完整接收文件")
	}
	if t.Direction == "in" && (t.Folder || len(t.Files) > 1) && t.Root != "" {
		folder = t.Root
	} else {
		if len(t.Files) == 0 {
			return "", "", errors.New("此消息没有本机文件位置")
		}
		selected = t.Files[0].Source
		if t.Direction == "in" {
			selected = t.Files[0].Final
		}
		if selected == "" {
			return "", "", errors.New("文件尚未保存到本机")
		}
		selected, err = filepath.Abs(selected)
		if err != nil {
			return "", "", err
		}
		folder = filepath.Dir(selected)
	}
	folder, err = filepath.Abs(folder)
	if err != nil {
		return "", "", err
	}
	if st, e := os.Stat(folder); e != nil || !st.IsDir() {
		return "", "", errors.New("所在文件夹已移动或无法访问")
	}
	if selected != "" {
		if _, e := os.Stat(selected); e != nil {
			selected = ""
		}
	}
	return folder, selected, nil
}
