package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTaskExportPathsAndBoundaries(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "素材😀")
	os.MkdirAll(filepath.Join(folder, "子目录"), 0700)
	a := filepath.Join(folder, "子目录", "图.png")
	b := filepath.Join(folder, "文档.txt")
	os.WriteFile(a, []byte("image"), 0600)
	os.WriteFile(b, []byte("text"), 0600)
	files, _, _, e := collect([]string{folder})
	if e != nil {
		t.Fatal(e)
	}
	n := &Node{cfg: Config{Tasks: map[string]*Task{}}}
	n.cfg.Tasks["out"] = &Task{Direction: "out", Folder: true, Files: files}
	paths, e := n.exportTaskPaths("out")
	if e != nil || !reflect.DeepEqual(paths, []string{folder}) {
		t.Fatalf("folder: %v %v", paths, e)
	}
	n.cfg.Tasks["out"].Folder = false
	paths, e = n.exportTaskPaths("out")
	if e != nil || len(paths) != 2 {
		t.Fatal(paths, e)
	}
	n.cfg.Tasks["text"] = &Task{Kind: "text", Direction: "out", Files: files}
	if _, e = n.exportTaskPaths("text"); e == nil {
		t.Fatal("exported cached text")
	}
	n.cfg.Tasks["in"] = &Task{Direction: "in", Status: "receiving", Root: folder, Folder: true, Files: []Meta{{Final: a}, {Final: b}}}
	if _, e = n.exportTaskPaths("in"); e == nil {
		t.Fatal("exported unfinished incoming file")
	}
	n.cfg.Tasks["in"].Status = "done"
	paths, e = n.exportTaskPaths("in")
	if e != nil || !reflect.DeepEqual(paths, []string{folder}) {
		t.Fatal(paths, e)
	}
	outside := filepath.Join(dir, "另一个任务.txt")
	os.WriteFile(outside, []byte("other"), 0600)
	n.cfg.Tasks["in"].Files[1].Final = outside
	if _, e = n.exportTaskPaths("in"); e == nil {
		t.Fatal("exported another task outside root")
	}
	n.cfg.Tasks["in"].Files = []Meta{{Final: a}}
	n.cfg.Tasks["in"].Folder = false
	n.cfg.Tasks["in"].Root = dir
	paths, e = n.exportTaskPaths("in")
	if e != nil || !reflect.DeepEqual(paths, []string{a}) {
		t.Fatal("single file exported shared save directory", paths, e)
	}
	os.Remove(a)
	if _, e = n.exportTaskPaths("in"); e == nil {
		t.Fatal("exported missing file")
	}
}
