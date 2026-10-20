package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileLocationFollowsSavedCollisionAndSurvivesMissingFile(t *testing.T) {
	old, current := t.TempDir(), t.TempDir()
	file := filepath.Join(old, "视频😀, (1).mp4")
	if err := os.WriteFile(file, []byte("example"), 0600); err != nil {
		t.Fatal(err)
	}
	n := &Node{cfg: Config{SaveDir: current, Tasks: map[string]*Task{"saved": {Direction: "in", Status: "done", Root: old, Files: []Meta{{Final: file}}}}}}
	folder, selected, err := n.taskFileLocation("saved")
	if err != nil || folder != old || selected != file {
		t.Fatal("historical file followed changed default folder", folder, selected, err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	folder, selected, err = n.taskFileLocation("saved")
	if err != nil || folder != old || selected != "" {
		t.Fatal("missing file should still allow its existing folder", folder, selected, err)
	}
	n.cfg.Tasks["saved"].Folder = true
	folder, selected, err = n.taskFileLocation("saved")
	if err != nil || folder != old || selected != "" {
		t.Fatal("received folder location", folder, selected, err)
	}
	n.cfg.Tasks["saved"].Status = "receiving"
	if _, _, err := n.taskFileLocation("saved"); err == nil {
		t.Fatal("unfinished file exposed as saved")
	}
}
