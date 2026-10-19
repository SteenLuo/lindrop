package main

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var guiNotices incomingNotices
var guiNoticePeer string
var guiLastNoticeSound time.Time
var noticeSettingLabels = []string{"提示音", "设备未读角标", "标题未读数", "任务栏提示", "系统通知", "窗口内提示"}

func toggleNoticeSetting(index int) {
	s := guiNode.noticeSettings()
	s.toggle(index)
	if e := guiNode.SetNoticeSettings(s); e != nil {
		showError(e.Error())
		return
	}
	if !s.InApp {
		chatFeedbackUntil = time.Time{}
	}
	if !s.Taskbar {
		flash := struct {
			Size                  uint32
			Hwnd                  uintptr
			Flags, Count, Timeout uint32
		}{Hwnd: guiMain}
		flash.Size = uint32(unsafe.Sizeof(flash))
		call(user.NewProc("FlashWindowEx"), uintptr(unsafe.Pointer(&flash)))
	}
	if !s.Popup {
		tray := guiTray
		tray.Flags = 0x10
		tray.Info = [256]uint16{}
		call(shell.NewProc("Shell_NotifyIconW"), 1, uintptr(unsafe.Pointer(&tray)))
	}
	updateNoticeControls()
	refreshIncomingNotices(guiAllTasks)
	invalidate(guiMain)
	invalidate(guiControls[111])
	invalidate(guiControls[201])
}
func updateNoticeControls() {
	for i, on := range guiNode.noticeSettings().values() {
		state := "关闭"
		if on {
			state = "开启"
		}
		set(guiControls[260+i], noticeSettingLabels[i]+"："+state)
		invalidate(guiControls[260+i])
	}
}

func viewedChatPeer() string {
	if guiPage != 0 || guiSelected == "" || call(user.NewProc("GetForegroundWindow")) != guiMain || call(user.NewProc("IsWindowVisible"), guiMain) == 0 || call(user.NewProc("IsIconic"), guiMain) != 0 {
		return ""
	}
	if len(guiTasks) > 0 {
		var last, client rect
		h := guiControls[126]
		msg(h, 0x198, uintptr(len(guiTasks)-1), uintptr(unsafe.Pointer(&last)))
		call(user.NewProc("GetClientRect"), h, uintptr(unsafe.Pointer(&client)))
		if last.B > client.B+int32(sc(30)) {
			return ""
		}
	}
	return guiSelected
}
func refreshIncomingNotices(tasks []Task) {
	before := guiNotices.count("")
	viewed := viewedChatPeer()
	arrived := guiNotices.update(tasks, guiNode.id, viewed)
	after := guiNotices.count("")
	settings := guiNode.noticeSettings()
	if before != after || len(arrived) > 0 {
		invalidate(guiControls[111])
		invalidate(guiControls[201])
	}
	title := guiWindowTitle
	if after > 0 && settings.Title {
		title = fmt.Sprintf("%s · %d条未读", title, after)
	}
	set(guiMain, title)
	if len(arrived) == 0 {
		return
	}
	latest := arrived[0] // snapshot has deterministic newest-first order.
	peer := conversationPeer(latest, guiNode.id)
	name := "设备"
	for _, d := range guiDevices {
		if d.ID == peer {
			name = deviceName(d)
			break
		}
	}
	kind := "文件"
	if latest.Kind == "text" {
		kind = "文字"
	}
	noticeText := fmt.Sprintf("收到 %s 的新%s", name, kind)
	if settings.InApp {
		chatFeedback = noticeText
		chatFeedbackUntil = time.Now().Add(5 * time.Second)
		invalidate(guiMain)
	}
	if settings.Sound && time.Since(guiLastNoticeSound) > 1500*time.Millisecond {
		call(user.NewProc("MessageBeep"), 0x40)
		guiLastNoticeSound = time.Now()
	}
	if peer == viewed {
		return
	}
	guiNoticePeer = peer
	flash := struct {
		Size                  uint32
		Hwnd                  uintptr
		Flags, Count, Timeout uint32
	}{Hwnd: guiMain, Flags: 2, Count: 3}
	flash.Size = uint32(unsafe.Sizeof(flash))
	if settings.Taskbar {
		call(user.NewProc("FlashWindowEx"), uintptr(unsafe.Pointer(&flash)))
	}
	tray := guiTray
	tray.Flags = 0x10
	tray.InfoFlags = 1 | 0x10 // one explicit sound above.
	copy(tray.InfoTitle[:], syscall.StringToUTF16("邻传 · 新消息"))
	copy(tray.Info[:], syscall.StringToUTF16(noticeText+"，点击查看"))
	if settings.Popup {
		call(shell.NewProc("Shell_NotifyIconW"), 1, uintptr(unsafe.Pointer(&tray)))
	}
}
func openNoticeConversation() {
	guiPage = 0
	if guiNoticePeer != "" {
		for i, d := range guiDevices {
			if d.ID == guiNoticePeer {
				guiSelected = d.ID
				msg(guiControls[111], 0x186, uintptr(i), 0)
				break
			}
		}
	}
	guiLastTasks = ""
	showWindow.Call(guiMain, 9)
	call(user.NewProc("SetForegroundWindow"), guiMain)
	refreshGUI()
	layout()
	scrollChatBottom()
}
