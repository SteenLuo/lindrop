package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Opt-in, deterministic visual fixture. It is compiled only into the test
// program, never into the delivered app. All sample names match the approved A.
func TestDesktopVisualReference(t *testing.T) {
	dir := os.Getenv("LINNODE_VISUAL_DIR")
	if dir == "" {
		t.Skip("opt-in visual comparison")
	}
	n, e := NewNode(dir, "当前笔记本", filepath.Join(dir, "receive"))
	if e != nil {
		t.Fatal(e)
	}
	defer n.Close()
	if e = n.Start("127.0.0.1", 0); e != nil {
		t.Fatal(e)
	}
	guiSelected = "study"
	guiWindowTitle = "邻传 · 视觉校验（示例数据）"
	guiClientWidth, guiClientHeight = 1200, 654
	guiReadSnapshot = func() map[string]interface{} {
		return map[string]interface{}{"saveDir": n.cfg.SaveDir, "devices": []Device{
			{ID: "study", Name: "书房台式机", Online: true, Paired: true},
			{ID: "living", Name: "客厅电脑", Online: true, Paired: true, Via: "书房台式机"},
			{ID: "web:phone", Name: "小米手机", Online: true, Paired: true},
			{ID: "web:tablet", Name: "安卓平板", Online: true, Paired: true},
			{ID: "office", Name: "办公电脑", Paired: true},
		}, "tasks": []Task{
			{ID: "video", Title: "周末出游.mp4", Direction: "out", Sender: n.id, Target: "study", TargetName: "书房台式机", Status: "sending", Total: 2576980377, Done: 1696512081, Rate: 40475033},
			{ID: "zip", Title: "项目素材.zip", Direction: "in", Sender: "study", Target: n.id, SenderName: "书房台式机", Status: "done", Total: 509607936, Done: 509607936},
			{ID: "document", Title: "会议记录.docx", Direction: "in", Sender: "study", Target: n.id, SenderName: "书房台式机", Status: "waiting", Total: 2936012},
		}}
	}
	guiReadShares = func(d Device) ([]Share, error) {
		return []Share{{ID: "video", Name: "家庭录像.mp4", Size: 1288490188, Files: make([]Meta, 1)}, {ID: "folder", Name: "公共素材", Size: 713031680, Folder: true, Files: make([]Meta, 24)}}, nil
	}
	raw, _ := json.Marshal(map[string]string{"local": n.localAddr, "admin": n.admin, "kind": "isolated-visual-fixture"})
	if e = os.WriteFile(filepath.Join(dir, "control.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	runDesktop(n)
}
