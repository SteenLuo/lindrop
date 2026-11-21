package main

import (
	"os"
	"path/filepath"
	"testing"
)

func folderBatchFixture(t *testing.T) ([]string, map[string]string) {
	t.Helper()
	root := t.TempDir()
	first, second := filepath.Join(root, "甲", "素材😀"), filepath.Join(root, "乙", "素材😀")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(filepath.Join(dir, "子目录"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	standalone := filepath.Join(root, "素材😀")
	contents := map[string]string{
		filepath.Join(first, "子目录", "同名.txt"):  "第一个文件夹的内容",
		filepath.Join(second, "子目录", "同名.txt"): "第二个文件夹的不同内容",
		standalone: "单独拖入的文件",
	}
	for path, body := range contents {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return []string{first, second, standalone}, map[string]string{
		"素材😀/子目录/同名.txt":     "第一个文件夹的内容",
		"素材😀 (2)/子目录/同名.txt": "第二个文件夹的不同内容",
		"素材😀 (3)":            "单独拖入的文件",
	}
}

func TestFolderBatchKeepsRootsAndTransfersWithoutNameCollisions(t *testing.T) {
	paths, expected := folderBatchFixture(t)
	files, folder, _, err := collect(paths)
	if err != nil || folder || len(files) != len(expected) {
		t.Fatal("folder batch manifest", files, folder, err)
	}
	for _, f := range files {
		body, ok := expected[f.Rel]
		actual, e := os.ReadFile(f.Source)
		if !ok || e != nil || string(actual) != body {
			t.Fatal("root folder was flattened or contents mixed", f.Rel, e)
		}
	}
	single, singleFolder, _, err := collect(paths[:1])
	if err != nil || !singleFolder || len(single) != 1 || single[0].Rel != "子目录/同名.txt" {
		t.Fatal("single folder layout changed", single, err)
	}
	a, b := isolatedTransferNode(t, "批量发送"), isolatedTransferNode(t, "批量接收")
	connect(t, a, b)
	id, err := a.Send(paths, b.id)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return !a.busy[id] && a.cfg.Tasks[id].Status == "waiting_remote"
	})
	if err = b.Accept(id, false); err != nil {
		t.Fatal("receiver rejected batch manifest", err)
	}
	if err = a.sendTask(id); err != nil {
		t.Fatal(err)
	}
	if !taskDone(a, id) || !taskDone(b, id) {
		t.Fatal("folder batch did not complete")
	}
	b.mu.Lock()
	received := append([]Meta(nil), b.cfg.Tasks[id].Files...)
	root := b.cfg.Tasks[id].Root
	b.mu.Unlock()
	for _, f := range received {
		actual, e := os.ReadFile(f.Final)
		rel, e2 := filepath.Rel(root, f.Final)
		if e != nil || e2 != nil || filepath.ToSlash(rel) != f.Rel || string(actual) != expected[f.Rel] {
			t.Fatal("received folder hierarchy or content mismatch", f.Rel, f.Final, e, e2)
		}
		if hash, err := shaFile(f.Final); err != nil || hash != hashBytes(actual) {
			t.Fatal("received SHA mismatch", err)
		}
	}
}
