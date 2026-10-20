package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	qrcode "github.com/skip2/go-qrcode"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var user = syscall.NewLazyDLL("user32.dll")
var gdi = syscall.NewLazyDLL("gdi32.dll")
var createWindow = user.NewProc("CreateWindowExW")
var sendMessage = user.NewProc("SendMessageW")
var postMessage = user.NewProc("PostMessageW")
var setText = user.NewProc("SetWindowTextW")
var getText = user.NewProc("GetWindowTextW")
var showWindow = user.NewProc("ShowWindow")
var destroyWindow = user.NewProc("DestroyWindow")
var guiMain uintptr

type point struct{ X, Y int32 }
type rect struct{ L, T, R, B int32 }
type winClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	SmallIcon                          uintptr
}
type winMsg struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	Private        uint32
}
type drawItem struct {
	Type, ID, ItemID, Action, State uint32
	Hwnd, DC                        uintptr
	Rect                            rect
	Data                            uintptr
}
type trayData struct {
	Size               uint32
	Hwnd               uintptr
	ID, Flags, Message uint32
	Icon               uintptr
	Tip                [128]uint16
	State, StateMask   uint32
	Info               [256]uint16
	Version            uint32
	InfoTitle          [64]uint16
	InfoFlags          uint32
	GUID               [16]byte
	BalloonIcon        uintptr
}

var guiNode *Node
var guiControls = map[int]uintptr{}
var guiDevices []Device
var guiTasks []Task
var guiShares []Share
var guiSelected string
var guiEvents = make(chan func(), 64)
var guiLastDevices, guiLastTasks string
var guiFont, guiBold, guiBrush uintptr
var guiScale = 1.0
var guiTray trayData
var qrBitmap uintptr
var qrWindow uintptr

func desktopClass(dir string) string {
	abs, _ := filepath.Abs(dir)
	return "LinNodeDesktop-" + hashBytes([]byte(strings.ToLower(abs)))[:20]
}
func acquireInstance(dir string) (uintptr, bool) {
	name := "Local\\" + desktopClass(dir)
	h, _, e := kernel.NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(wp(name))))
	return h, h != 0 && e != syscall.ERROR_ALREADY_EXISTS
}
func activateExisting(dir string) {
	h := call(user.NewProc("FindWindowW"), uintptr(unsafe.Pointer(wp(desktopClass(dir)))), 0)
	if h != 0 {
		showWindow.Call(h, 9)
		call(user.NewProc("SetForegroundWindow"), h)
	}
}

func wp(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

//go:uintptrescapes
func call(p *syscall.LazyProc, args ...uintptr) uintptr { v, _, _ := p.Call(args...); return v }
func sc(v int) int                                      { return int(float64(v) * guiScale) }

func setWindowProcedure(h, callback uintptr) uintptr {
	name := "SetWindowLongW"
	if unsafe.Sizeof(uintptr(0)) == 8 {
		name = "SetWindowLongPtrW"
	}
	index := int32(-4) // GWLP_WNDPROC is pointer-sized on Win64.
	return call(user.NewProc(name), h, uintptr(index), callback)
}

func set(h uintptr, s string) {
	if h != 0 && text(h) != s {
		setText.Call(h, uintptr(unsafe.Pointer(wp(s))))
	}
}
func text(h uintptr) string {
	length := call(user.NewProc("GetWindowTextLengthW"), h)
	if length > maxTextBytes*2 {
		length = maxTextBytes * 2
	}
	buf := make([]uint16, int(length)+1)
	getText.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

//go:uintptrescapes
func msg(h uintptr, m uint32, w, l uintptr) uintptr { return call(sendMessage, h, uintptr(m), w, l) }
func control(class, label string, id int, style uint32) uintptr {
	if class == "BUTTON" {
		style = (style &^ 15) | 11
	}
	if class != "STATIC" {
		style |= 0x10000
	}
	h := call(createWindow, 0, uintptr(unsafe.Pointer(wp(class))), uintptr(unsafe.Pointer(wp(label))), uintptr(0x50000000|style), 0, 0, 0, 0, guiMain, uintptr(id), 0, 0)
	guiControls[id] = h
	msg(h, 0x30, guiFont, 1)
	return h
}
func move(id, x, y, w, h int) {
	if old, ok := guiPositions[id]; ok && old == [4]int{x, y, w, h} {
		return
	}
	guiPositions[id] = [4]int{x, y, w, h}
	call(user.NewProc("MoveWindow"), guiControls[id], uintptr(sc(x)), uintptr(sc(y)), uintptr(sc(w)), uintptr(sc(h)), 1)
}
func cue(id int, s string) { msg(guiControls[id], 0x1501, 0, uintptr(unsafe.Pointer(wp(s)))) }
func queue(fn func()) {
	select {
	case guiEvents <- fn:
		postMessage.Call(guiMain, 0x8001, 0, 0)
	case <-guiNode.stop:
	}
}
func work(fn func() error) {
	go func() {
		e := fn()
		queue(func() {
			if e != nil {
				set(guiControls[190], e.Error())
				showError(e.Error())
			} else {
				set(guiControls[190], "操作已完成")
				refreshGUI()
			}
		})
	}()
}
func showError(s string) {
	user.NewProc("MessageBoxW").Call(guiMain, uintptr(unsafe.Pointer(wp(s))), uintptr(unsafe.Pointer(wp("邻传"))), 0x10)
}
func statusText(t Task) string {
	if t.Status == "verifying" {
		return "校验中 · " + formatSize(t.Done) + " / " + formatSize(t.Total)
	}
	names := map[string]string{"preparing": "校验源文件", "waiting": "待接收确认", "waiting_remote": "等待对方接收", "receiving": "接收中", "sending": "发送中", "paused": "已暂停", "queued": "等待继续", "done": "已完成", "rejected": "已拒绝", "browser": "等待手机下载", "waiting_browser": "等待手机点击下载"}
	s := names[t.Status]
	if s == "" {
		s = t.Status
	}
	s += " · " + formatSize(t.Done) + " / " + formatSize(t.Total)
	if t.Rate > 0 {
		s += " · " + formatSize(int64(t.Rate)) + "/s"
		if t.Total > t.Done {
			s += fmt.Sprintf(" · 约剩 %d 秒", int64(float64(t.Total-t.Done)/t.Rate))
		}
	}
	if t.Error != "" {
		s += " · " + t.Error
	}
	return s
}
func resetList(h uintptr, count int) {
	top := msg(h, 0x18e, 0, 0)
	msg(h, 0xb, 0, 0)
	msg(h, 0x184, 0, 0)
	for i := 0; i < count; i++ {
		label := " "
		if h == guiControls[111] && i < len(guiDevices) {
			label = guiDevices[i].Name
		}
		if h == guiControls[126] && i < len(guiTasks) {
			label = guiTasks[i].Title + " · " + statusText(guiTasks[i])
		}
		if h == guiControls[143] && i < len(guiShares) {
			label = guiShares[i].Name
		}
		msg(h, 0x180, 0, uintptr(unsafe.Pointer(wp(label))))
	}
	if count > 0 {
		msg(h, 0x197, top, 0)
	}
	msg(h, 0xb, 1, 0)
	invalidate(h)
}
func currentDevice() (Device, bool) {
	index := int(int32(msg(guiControls[111], 0x188, 0, 0)))
	if index >= 0 && index < len(guiDevices) {
		return guiDevices[index], true
	}
	if guiSelected == guiNode.id {
		return Device{ID: guiNode.id, Name: guiNode.name(), Online: true, Paired: true, Addr: guiNode.addr}, true
	}
	return Device{}, false
}
func currentTask() (Task, bool) {
	index := int(int32(msg(guiControls[126], 0x188, 0, 0)))
	if index >= 0 && index < len(guiTasks) {
		return guiTasks[index], true
	}
	return Task{}, false
}
func refreshShares() {
	d, ok := currentDevice()
	if !ok {
		set(guiControls[190], "请先选择一台设备")
		return
	}
	guiSelected = d.ID
	if !d.Online || strings.HasPrefix(d.ID, "web:") {
		guiShares = nil
		resetList(guiControls[143], 0)
		if strings.HasPrefix(d.ID, "web:") {
			set(guiControls[140], "网页设备：发送后由对方点击下载")
			set(guiControls[190], "选择发送文件即可，不需要向手机建立入站连接")
		} else {
			set(guiControls[140], "尚未连接，正在自动重试")
			set(guiControls[190], "可在连接其他设备中填写对方地址，无需配对码")
		}
		layout()
		return
	}
	work(func() error {
		var shares []Share
		if guiReadShares != nil {
			var e error
			shares, e = guiReadShares(d)
			if e != nil {
				return e
			}
		} else if e := guiNode.Call(d.ID, "shares", struct{}{}, &shares); e != nil {
			return e
		}
		queue(func() {
			if guiSelected != d.ID {
				return
			}
			guiShares = shares
			resetList(guiControls[143], len(shares))
			set(guiControls[140], d.Name+" · 共享文件")
			layout()
		})
		return nil
	})
}
func selectPaths(folder bool) ([]string, error) {
	if !guiNode.picker.TryLock() {
		return nil, fmt.Errorf("已有文件窗口打开")
	}
	defer guiNode.picker.Unlock()
	if folder {
		p, e := pickFolder()
		if p == "" {
			return nil, e
		}
		return []string{p}, e
	}
	return pickFiles()
}
func doCommand(id, notification int) {
	if id >= 260 && id <= 265 {
		toggleNoticeSetting(id - 260)
		return
	}
	switch id {
	case 201, 202, 203:
		guiPage = id - 201
		layout()
		if guiPage == 1 {
			refreshShares()
		}
	case 204:
		showConnect()
	case 205, 246:
		guiSelected = guiNode.id
		msg(guiControls[111], 0x186, ^uintptr(0), 0)
		guiPage = 1
		layout()
		refreshShares()
	case 210, 211, 212:
		guiFilter = id - 210
		guiLastTasks = ""
		refreshGUI()
	case 126, 143:
		if id == 126 && notification == 2 {
			if t, ok := currentTask(); ok && t.Kind == "text" {
				showChatSelection()
			} else if ok {
				openChatAttachment(t)
			}
			return
		}
		if notification == 1 {
			updateContext()
		}
	case 103:
		showQR()
	case 106:
		name := text(guiControls[105])
		work(func() error { return guiNode.SetName(name) })
	case 111:
		if notification == 1 {
			if d, ok := currentDevice(); ok {
				guiSelected = d.ID
				if !d.Online && d.Addr != "" {
					guiNode.connectNearby(Peer{d.ID, d.Name, d.Addr})
				}
				switchChatDraft(d.ID)
				guiLastTasks = ""
				refreshGUI()
				if !d.Online && d.Addr != "" {
					set(guiControls[113], d.Addr)
				}
				guiShares = nil
				resetList(guiControls[143], 0)
				layout()
				if guiPage == 1 {
					refreshShares()
				}
			}
		}
	case 115:
		addr, code := text(guiControls[113]), ""
		var field int
		addr, code, field, guiPairError = desktopPairInput(addr, code)
		invalidate(guiConnect)
		if field != 0 {
			call(user.NewProc("SetFocus"), guiControls[field])
			return
		}
		set(guiControls[190], "正在建立节点连接…")
		work(func() error {
			if e := guiNode.Dial(addr, code); e != nil {
				return desktopPairError(e)
			}
			queue(func() {
				if guiConnect != 0 {
					destroyWindow.Call(guiConnect)
				}
			})
			return nil
		})
	case 116:
		hideQR()
		showWindow.Call(guiMain, 0)
	case 121, 122:
		d, ok := currentDevice()
		if e := desktopSendTarget(d, ok, guiNode.id); e != nil {
			showError(e.Error())
			return
		}
		set(guiControls[190], "请选择要发送的文件，准备完成后等待对方确认")
		work(func() error {
			paths, e := selectPaths(id == 122)
			if e != nil || len(paths) == 0 {
				return e
			}
			return sendDesktopFiles(paths, d.ID)
		})
	case 171:
		sendChatText()
	case 174:
		showChatSelection()
	case 176:
		if t, ok := currentTask(); ok {
			openChatAttachment(t)
		}
	case 173:
		toggleChatText()
	case 172:
		if t, ok := currentTask(); ok {
			if t.Kind != "text" {
				copyChatFiles(t.ID)
				return
			}
			if e := copyDesktopText(selectedChatText(t)); e != nil {
				showError(e.Error())
			} else {
				set(guiControls[190], "已复制文字")
				chatFeedback = "已复制文字"
				chatFeedbackUntil = time.Now().Add(3 * time.Second)
				invalidate(guiMain)
			}
		}
	case 159:
		showTextComposer()
	case 164:
		if t, ok := currentTask(); ok {
			showTaskText(t)
		}
	case 123, 124:
		work(func() error {
			paths, e := selectPaths(id == 124)
			if e != nil || len(paths) == 0 {
				return e
			}
			if e := guiNode.AddShare(paths); e != nil {
				return e
			}
			queue(func() {
				if guiSelected == guiNode.id && guiPage == 1 {
					refreshShares()
				}
			})
			return nil
		})
	case 127, 128, 129, 130, 131:
		t, ok := currentTask()
		if !ok {
			showError("请先选择一个传输任务")
			return
		}
		actions := map[int]string{127: "accept", 128: "trust", 129: "reject", 130: "pause", 131: "resume"}
		work(func() error { return guiNode.TaskAction(t.ID, actions[id]) })
	case 141:
		refreshShares()
	case 142:
		d, ok := currentDevice()
		index := int(int32(msg(guiControls[143], 0x188, 0, 0)))
		if !ok || index < 0 || index >= len(guiShares) {
			showError("请选择一个共享文件或目录")
			return
		}
		s := guiShares[index]
		work(func() error { _, e := guiNode.Pull(d.ID, s.ID); return e })
	case 144:
		d, ok := currentDevice()
		index := int(int32(msg(guiControls[143], 0x188, 0, 0)))
		if !ok || d.ID != guiNode.id || index < 0 || index >= len(guiShares) {
			showError("请在本机共享列表中选择文件；移除分享会保留原文件")
			return
		}
		s := guiShares[index]
		work(func() error {
			guiNode.mu.Lock()
			delete(guiNode.cfg.Shares, s.ID)
			guiNode.mu.Unlock()
			if e := guiNode.persist(); e != nil {
				return e
			}
			queue(refreshShares)
			return nil
		})
	case 153:
		if t, ok := currentTask(); ok {
			folder, selected, e := guiNode.taskFileLocation(t.ID)
			if e == nil {
				e = revealInFolder(folder, selected)
			}
			if e != nil {
				showError(e.Error())
			}
		}
	case 150, 154:
		guiNode.mu.Lock()
		dir := guiNode.cfg.SaveDir
		guiNode.mu.Unlock()
		exec.Command("explorer.exe", dir).Start()
	case 151:
		work(func() error {
			paths, e := selectPaths(true)
			if e != nil || len(paths) == 0 {
				return e
			}
			return guiNode.SetFolder(paths[0])
		})
	case 152, 1002:
		destroyWindow.Call(guiMain)
	case 1001:
		showWindow.Call(guiMain, 9)
		user.NewProc("SetForegroundWindow").Call(guiMain)
	}
}
func color(r, g, b byte) uintptr { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }
func drawText(dc uintptr, s string, r rect, clr uintptr, font uintptr) {
	if vectorText(dc, s, r, clr, font, 0) {
		return
	}
	gdi.NewProc("SetTextColor").Call(dc, clr)
	gdi.NewProc("SetBkMode").Call(dc, 1)
	gdi.NewProc("SelectObject").Call(dc, font)
	user.NewProc("DrawTextW").Call(dc, uintptr(unsafe.Pointer(wp(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x20|0x800|0x4)
}
func mainProc(hwnd uintptr, message uint32, w, l uintptr) uintptr {
	switch message {
	case wmDropFiles:
		desktopDrop(w)
		return 0
	case 0x5:
		hideQR()
		if guiMain != 0 {
			layout()
		}
		return 0
	case 0x3:
		hideQR()
	case 0x1c:
		if w == 0 {
			hideQR()
		}
	case 0x111:
		doCommand(int(w&0xffff), int((w>>16)&0xffff))
		return 0
	case 0x14:
		return 1
	case 0x0f:
		paintMain(hwnd)
		return 0
	case 0x24:
		setMinimum(l)
		return 0
	case 0x2b:
		di := (*drawItem)(unsafe.Pointer(l))
		if di.Type == 4 {
			paintButton(di)
		} else {
			paintItem(di)
		}
		return 1
	case 0x2c:
		items := (*struct {
			Type, ID, ItemID, Width, Height uint32
			Data                            uintptr
		})(unsafe.Pointer(l))
		height := 75
		if items.ID == 126 {
			height = 116
			if index := int(items.ItemID); index >= 0 && index < len(guiTasks) {
				height = chatRowHeightAt(guiTasks[index], index)
			}
		}
		if items.ID == 143 {
			height = 84
		}
		items.Height = uint32(sc(height))
		return 1
	case 0x113:
		select {
		case <-guiNode.stop:
			destroyWindow.Call(hwnd)
			return 0
		default:
		}
		refreshGUI()
		return 0
	case 0x138, 0x133:
		call(gdi.NewProc("SetTextColor"), w, ink)
		call(gdi.NewProc("SetBkColor"), w, paper)
		return guiWhiteBrush
	case 0x134:
		if l == guiControls[111] {
			call(gdi.NewProc("SetBkColor"), w, paper)
			return guiWhiteBrush
		}
		call(gdi.NewProc("SetBkColor"), w, canvas)
		return guiBrush
	case 0x8001:
		for {
			select {
			case fn := <-guiEvents:
				fn()
				invalidate(guiMain)
			default:
				return 0
			}
		}
	case 0x8004:
		if l == 0x405 {
			openNoticeConversation()
			return 0
		}
		if l == 0x202 {
			showWindow.Call(hwnd, 9)
			call(user.NewProc("SetForegroundWindow"), hwnd)
		}
		if l == 0x205 {
			menu := call(user.NewProc("CreatePopupMenu"))
			call(user.NewProc("AppendMenuW"), menu, 0, 1001, uintptr(unsafe.Pointer(wp("打开邻传"))))
			call(user.NewProc("AppendMenuW"), menu, 0, 1002, uintptr(unsafe.Pointer(wp("退出"))))
			var pt point
			call(user.NewProc("GetCursorPos"), uintptr(unsafe.Pointer(&pt)))
			call(user.NewProc("SetForegroundWindow"), hwnd)
			call(user.NewProc("TrackPopupMenu"), menu, 0x2, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
			call(user.NewProc("DestroyMenu"), menu)
		}
		return 0
	case 0x10:
		hideQR()
		showWindow.Call(hwnd, 0)
		return 0
	case 0x2:
		if guiTextWindow != 0 {
			destroyWindow.Call(guiTextWindow)
		}
		destroyQR()
		call(shell.NewProc("Shell_NotifyIconW"), 2, uintptr(unsafe.Pointer(&guiTray)))
		user.NewProc("PostQuitMessage").Call(0)
		return 0
	}
	return call(user.NewProc("DefWindowProcW"), hwnd, uintptr(message), w, l)
}
func createQRBitmap() bool {
	if qrBitmap != 0 {
		return true
	}
	url := "http://" + guiNode.addr + "/#join=" + guiNode.webToken
	raw, e := qrcode.Encode(url, qrcode.Medium, sc(220))
	if e != nil {
		showError(e.Error())
		return false
	}
	img, e := png.Decode(bytes.NewReader(raw))
	if e != nil {
		showError(e.Error())
		return false
	}
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	stride := (width*3 + 3) &^ 3
	var bmp bytes.Buffer
	binary.Write(&bmp, binary.LittleEndian, uint16(0x4d42))
	binary.Write(&bmp, binary.LittleEndian, uint32(54+stride*height))
	binary.Write(&bmp, binary.LittleEndian, uint32(0))
	binary.Write(&bmp, binary.LittleEndian, uint32(54))
	binary.Write(&bmp, binary.LittleEndian, uint32(40))
	binary.Write(&bmp, binary.LittleEndian, int32(width))
	binary.Write(&bmp, binary.LittleEndian, int32(height))
	binary.Write(&bmp, binary.LittleEndian, uint16(1))
	binary.Write(&bmp, binary.LittleEndian, uint16(24))
	for i := 0; i < 6; i++ {
		binary.Write(&bmp, binary.LittleEndian, uint32(0))
	}
	for y := height - 1; y >= 0; y-- {
		row := make([]byte, stride)
		for x := 0; x < width; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			row[x*3] = byte(b >> 8)
			row[x*3+1] = byte(g >> 8)
			row[x*3+2] = byte(r >> 8)
		}
		bmp.Write(row)
	}
	path := filepath.Join(guiNode.dir, "phone.bmp")
	if e = os.WriteFile(path, bmp.Bytes(), 0600); e != nil {
		showError(e.Error())
		return false
	}
	qrBitmap = call(user.NewProc("LoadImageW"), 0, uintptr(unsafe.Pointer(wp(path))), 0, 0, 0, 0x10)
	return qrBitmap != 0
}
