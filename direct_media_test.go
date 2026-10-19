package main

import (
	"bytes"
	"encoding/json"
	"image"
	imgcolor "image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoveryConnectsNewNodesWithoutPairing(t *testing.T) {
	a, b := testNode(t, "新电脑A"), testNode(t, "新电脑B")
	raw, _ := json.Marshal(beacon{"linnode-1", b.id, b.name(), b.addr})
	if a.observeBeacon(raw, net.ParseIP("192.168.222.9")) {
		t.Fatal("mismatched beacon source accepted")
	}
	if !a.observeBeacon(raw, net.ParseIP("127.0.0.2")) {
		t.Fatal("valid discovery rejected")
	}
	waitFor(t, func() bool {
		for _, d := range a.devices() {
			if d.ID == b.id {
				return d.Online
			}
		}
		return false
	})
	for _, pair := range [][2]*Node{{a, b}, {b, a}} {
		id, err := pair[0].SendText("自动发现后直接发送", pair[1].id)
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool { return taskDone(pair[0], id) && taskDone(pair[1], id) })
	}
	p := filepath.Join(t.TempDir(), "免配对.bin")
	data := bytes.Repeat([]byte("direct"), 3000)
	os.WriteFile(p, data, 0600)
	id, err := a.Send([]string{p}, b.id)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		v := b.cfg.Tasks[id]
		return v != nil && v.Status == "waiting"
	})
	if err = b.Accept(id, false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return taskDone(a, id) && taskDone(b, id) })
	b.mu.Lock()
	dest := b.cfg.Tasks[id].Files[0].Final
	b.mu.Unlock()
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("unpaired transfer changed bytes")
	}
}
func TestMediaOwnershipRangeAndClassification(t *testing.T) {
	n := testNode(t, "媒体入口")
	first, second := browser(t, n, "手机甲"), browser(t, n, "手机乙")
	var state struct{ Client WebClient }
	resp, _ := first.Get("http://" + n.addr + "/api/state")
	json.NewDecoder(resp.Body).Decode(&state)
	resp.Body.Close()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 18, 12))
	img.Set(1, 1, imgcolor.RGBA{R: 200, A: 255})
	png.Encode(&buf, img)
	path := filepath.Join(t.TempDir(), "图片.png")
	os.WriteFile(path, buf.Bytes(), 0600)
	id, err := n.Send([]string{path}, "web:"+n.id+":"+state.Client.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { n.mu.Lock(); defer n.mu.Unlock(); return n.cfg.Tasks[id].Status != "preparing" })
	url := "http://" + n.addr + "/api/media?id=" + id
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Range", "bytes=3-14")
	resp, err = first.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || resp.Header.Get("Content-Type") != "image/png" || resp.Header.Get("Content-Disposition") != "inline" || !bytes.Equal(got, buf.Bytes()[3:15]) {
		t.Fatal("media range/headers changed", resp.Status)
	}
	n.mu.Lock()
	status := n.cfg.Tasks[id].Status
	n.mu.Unlock()
	if status == "done" {
		t.Fatal("preview pretended file downloaded")
	}
	resp, _ = second.Get(url)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("other phone read media")
	}
	resp, _ = http.Get(url)
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal("unauthenticated media read")
	}
	resp, _ = first.Get("http://" + n.addr + "/api/state")
	var snapshot struct{ Tasks []Task }
	json.NewDecoder(resp.Body).Decode(&snapshot)
	resp.Body.Close()
	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Media != "image" || len(snapshot.Tasks[0].Files) != 0 {
		t.Fatal("media metadata leaks paths or missing")
	}
	for _, c := range []struct {
		name   string
		folder bool
		count  int
		want   string
	}{{"a.JPG", false, 1, "image"}, {"a.mp4", false, 1, "video"}, {"x.mp4.exe", false, 1, "file"}, {"a.png", true, 1, "folder"}, {"a.png", false, 2, "files"}} {
		if mediaKind(c.name, c.folder, c.count) != c.want {
			t.Fatal(c)
		}
	}
}

func TestMediaRowsKeepPreviewAtDisplayScale(t *testing.T) {
	oldScale := guiScale
	defer func() { guiScale = oldScale }()
	task := Task{ID: "preview-geometry", Media: "image", Title: "image.png", Direction: "in", Status: "done"}
	for _, scale := range []float64{1, 1.25, 1.5, 2} {
		guiScale = scale
		parts := chatMessageParts(task, 650)
		total := 0
		for _, p := range parts {
			b := chatBubblePart(task, 650, &p)
			total += b.H
			h := b.Y + b.H
			if b.Last {
				h += 27
			}
			if sc(h) > 255 {
				t.Fatal("media row too tall", scale, h)
			}
		}
		if total != 180 || len(parts) == 0 || !parts[0].First || !parts[len(parts)-1].Last {
			t.Fatal("preview lost display area")
		}
	}
}
