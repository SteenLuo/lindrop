package main

import (
	"fmt"
	"image"
	"strings"
	"sync"
	"time"
	"unsafe"
)

const wmPreparedScreenshot = 0x8006

var chatComposerHeight int
var chatResizeActive bool
var chatResizeStartY, chatResizeStartHeight int
var chatPictures = map[string][]draftPicture{}
var chatPicturePending = map[string]int{}
var chatPictureWorkers sync.WaitGroup
var chatPreparedPictures = make(chan preparedScreenshot, maxDraftPictures*2)
var chatSendingPictures = map[string]bool{}
var chatReadScreenshot = readClipboardScreenshot

type preparedScreenshot struct {
	Peer    string
	Picture draftPicture
	Err     error
}

func currentComposerLayout() composerGeometry {
	return composerLayout(guiHeight, chatComposerHeight, len(chatPictures[chatDraftDevice]))
}

func composerFeedback(s string) {
	chatFeedback, chatFeedbackUntil = s, time.Now().Add(6*time.Second)
	invalidate(guiMain)
}

func pasteChatScreenshot() bool {
	shot, available, err := chatReadScreenshot(guiMain)
	if !available {
		return false
	}
	if err != nil {
		composerFeedback(err.Error())
		return true
	}
	d, ok := currentDevice()
	if err = desktopSendTarget(d, ok, guiNode.id); err != nil {
		composerFeedback(err.Error())
		return true
	}
	peer := d.ID // Never look up the selected device again in the worker.
	if chatSending {
		composerFeedback("正在发送，请稍后粘贴截图")
		return true
	}
	if len(chatPictures[peer])+chatPicturePending[peer] >= maxDraftPictures {
		composerFeedback("一次最多准备8张截图，请先发送或移除部分截图")
		return true
	}
	// Bound all concurrently copied/decoded clipboard images, across peers.
	for _, n := range chatPicturePending {
		if n > 0 {
			composerFeedback("正在准备截图，请稍后再粘贴下一张")
			return true
		}
	}
	chatPicturePending[peer]++
	composerFeedback("正在准备截图…")
	stateDir := guiNode.dir
	chatPictureWorkers.Add(1)
	go func() {
		defer chatPictureWorkers.Done()
		var img image.Image
		var e error
		if shot.PNG {
			img, e = decodeScreenshotPNG(shot.Raw)
		} else {
			img, e = decodeScreenshotDIB(shot.Raw)
		}
		var p draftPicture
		if e == nil {
			p, e = saveDraftScreenshot(stateDir, img)
		}
		chatPreparedPictures <- preparedScreenshot{peer, p, e}
		postMessage.Call(guiMain, wmPreparedScreenshot, 0, 0)
	}()
	return true
}

func receivePreparedScreenshots() {
	for {
		select {
		case ready := <-chatPreparedPictures:
			chatPicturePending[ready.Peer]--
			if ready.Err == nil {
				chatPictures[ready.Peer] = append(chatPictures[ready.Peer], ready.Picture)
			}
			if ready.Peer == chatDraftDevice {
				if ready.Err != nil {
					composerFeedback(ready.Err.Error())
				} else {
					composerFeedback("截图已准备，点击发送或按 Ctrl + Enter")
				}
				layout()
			}
		default:
			return
		}
	}
}

func disposeDraftPicture(p draftPicture) {
	if cached := chatMediaImages[p.Path]; cached.image != 0 {
		call(vector.NewProc("GdipDisposeImage"), cached.image)
		delete(chatMediaImages, p.Path)
	}
	removeDraftScreenshot(guiNode.dir, p)
}

func closeChatComposer() {
	chatPictureWorkers.Wait()
	for {
		select {
		case ready := <-chatPreparedPictures:
			removeDraftScreenshot(guiNode.dir, ready.Picture)
		default:
			for _, pictures := range chatPictures {
				for _, p := range pictures {
					// A send worker may still be queueing these files. Keep them.
					if !chatSendingPictures[p.ID] {
						disposeDraftPicture(p)
					}
				}
			}
			return
		}
	}
}

func pictureDraftRect(index, count int) rect {
	width := (guiWidth - 370 - (count-1)*8) / count
	if width > 80 {
		width = 80
	}
	return rct(343+index*(width+8), currentComposerLayout().PreviewTop+2, width, 66)
}

func paintComposerPictures(dc uintptr) {
	pictures := chatPictures[chatDraftDevice]
	for i, p := range pictures {
		r := pictureDraftRect(i, len(pictures))
		rounded(dc, r, tint, lineColor, 7)
		area := rect{r.L + int32(sc(3)), r.T + int32(sc(3)), r.R - int32(sc(3)), r.B - int32(sc(3))}
		if !drawChatImage(dc, loadChatImage(p.Path), area) {
			centerText(dc, "截图", area, accent, guiTiny)
		}
		close := rect{r.R - int32(sc(20)), r.T, r.R, r.T + int32(sc(20))}
		rounded(dc, close, paper, lineColor, 6)
		centerText(dc, "×", close, ink, guiSmall)
	}
}

func composerMouse(hwnd uintptr, message uint32, w, l uintptr) bool {
	if guiPage != 0 {
		if chatResizeActive {
			chatResizeActive = false
			if call(user.NewProc("GetCapture")) == hwnd {
				call(user.NewProc("ReleaseCapture"))
			}
		}
		return false
	}
	x, y := int(float64(int16(l&0xffff))/guiScale), int(float64(int16((l>>16)&0xffff))/guiScale)
	g := currentComposerLayout()
	switch message {
	case 0x20: // WM_SETCURSOR carries no client position.
		if int16(l&0xffff) != 1 {
			return false
		}
		var p point
		call(user.NewProc("GetCursorPos"), uintptr(unsafe.Pointer(&p)))
		call(user.NewProc("ScreenToClient"), hwnd, uintptr(unsafe.Pointer(&p)))
		x, y = int(float64(p.X)/guiScale), int(float64(p.Y)/guiScale)
		if chatResizeActive || x >= 318 && y >= g.Top-4 && y <= g.Top+5 {
			call(user.NewProc("SetCursor"), call(user.NewProc("LoadCursorW"), 0, 32645))
			return true
		}
	case 0x201:
		if x >= 318 && y >= g.Top-4 && y <= g.Top+5 {
			chatResizeStartY, chatResizeStartHeight = y, g.TextHeight
			chatResizeActive = true
			call(user.NewProc("SetCapture"), hwnd)
			return true
		}
		for i, p := range chatPictures[chatDraftDevice] {
			r := pictureDraftRect(i, len(chatPictures[chatDraftDevice]))
			px, py := int32(sc(x)), int32(sc(y))
			if px >= r.R-int32(sc(20)) && px < r.R && py >= r.T && py < r.T+int32(sc(20)) {
				if chatSendingPictures[p.ID] {
					composerFeedback("这张截图正在发送")
				} else {
					chatPictures[chatDraftDevice] = withoutDraftPicture(chatPictures[chatDraftDevice], p.ID)
					disposeDraftPicture(p)
					layout()
				}
				return true
			}
		}
	case 0x200:
		if chatResizeActive {
			requested := chatResizeStartHeight + chatResizeStartY - y
			if requested < 64 {
				requested = 64
			}
			chatComposerHeight = composerLayout(guiHeight, requested, len(chatPictures[chatDraftDevice])).TextHeight
			layout()
			return true
		}
	case 0x202:
		if chatResizeActive {
			chatResizeActive = false
			call(user.NewProc("ReleaseCapture"))
			return true
		}
	case 0x215, 0x1f: // Lost capture / WM_CANCELMODE.
		if chatResizeActive {
			chatResizeActive = false
			if call(user.NewProc("GetCapture")) == hwnd {
				call(user.NewProc("ReleaseCapture"))
			}
			return true
		}
	}
	return false
}

// Keep the successful prefix separate: on partial error it must not be resent.
func sendComposerContent(n *Node, body, target string, pictures []draftPicture) (textID string, sent []string, err error) {
	if strings.TrimSpace(body) != "" {
		textID, err = n.SendText(body, target)
		if err != nil {
			return
		}
	}
	for _, p := range pictures {
		if _, err = n.Send([]string{p.Path}, target); err != nil {
			err = fmt.Errorf("部分内容尚未发送，已保留草稿：%w", err)
			return
		}
		sent = append(sent, p.ID)
	}
	return
}
