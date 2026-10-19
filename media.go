package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func mediaKind(name string, folder bool, count int) string {
	if folder {
		return "folder"
	}
	if count != 1 {
		return "files"
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp":
		return "image"
	case ".mp4", ".m4v", ".mov", ".webm", ".avi", ".wmv", ".mkv":
		return "video"
	default:
		return "file"
	}
}
func taskMediaKind(t Task) string {
	if t.Media != "" {
		return t.Media
	}
	if t.Kind == "text" {
		return "text"
	}
	return mediaKind(t.Title, t.Folder, len(t.Files))
}
func mediaContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".webp":
		return "image/webp"
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	case ".avi":
		return "video/x-msvideo"
	case ".wmv":
		return "video/x-ms-wmv"
	case ".mkv":
		return "video/x-matroska"
	}
	return "application/octet-stream"
}
func (n *Node) localAttachment(id string) (string, string, error) {
	n.mu.Lock()
	t := n.cfg.Tasks[id]
	if t == nil || t.Kind == "text" || t.Folder || len(t.Files) != 1 {
		n.mu.Unlock()
		return "", "", errors.New("请选择单个已保存文件")
	}
	path := ""
	if t.Direction == "out" {
		path = t.Files[0].Source
	}
	if t.Direction == "in" && t.Status == "done" {
		path = t.Files[0].Final
	}
	kind := taskMediaKind(*t)
	n.mu.Unlock()
	if path == "" {
		return "", "", errors.New("文件接收完成后即可查看")
	}
	stat, err := os.Stat(path)
	if err != nil || !stat.Mode().IsRegular() {
		return "", "", errors.New("文件已移动或不存在")
	}
	return path, kind, nil
}

// The URL accepts only a task ID. Ownership is checked before reading any bytes.
func (n *Node) serveMedia(w http.ResponseWriter, r *http.Request, c WebClient, admin bool) {
	id := r.URL.Query().Get("id")
	n.mu.Lock()
	t := n.cfg.Tasks[id]
	sid := "web:" + n.id + ":" + c.ID
	if t == nil || (!admin && t.Client != c.ID && t.Target != sid) || t.Kind == "text" || t.Folder || len(t.Files) != 1 {
		n.mu.Unlock()
		apiError(w, 403, "无权预览此任务")
		return
	}
	kind := taskMediaKind(*t)
	if kind != "image" && kind != "video" {
		n.mu.Unlock()
		apiError(w, 415, "此文件不支持内联预览")
		return
	}
	targetIsPhone := t.Target == sid && (t.Direction == "web" || t.Direction == "out")
	n.mu.Unlock()
	if targetIsPhone {
		q := r.URL.Query()
		q.Set("inbox", id)
		q.Set("preview", "1")
		r.URL.RawQuery = q.Encode()
		n.downloadWeb(w, r, c)
		return
	}
	path, _, err := n.localAttachment(id)
	if err != nil {
		apiError(w, 404, err)
		return
	}
	w.Header().Set("Content-Type", mediaContentType(path))
	w.Header().Set("Content-Disposition", "inline")
	http.ServeFile(w, r, path)
}
