package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

type mediaImage struct {
	image         uintptr
	width, height uint32
	size, mod     int64
}

var chatMediaImages = map[string]mediaImage{}
var mediaWindow uintptr
var mediaPath, mediaType, mediaError string
var mediaButtons []uintptr
var videoDLL = syscall.NewLazyDLL("winmm.dll")
var videoOpened bool
var mfPlayer, mfHost uintptr
var mfPaused, mfCOM bool
var mfDLL = syscall.NewLazyDLL("mfplay.dll")

func mfCall(index int, args ...uintptr) uintptr {
	if mfPlayer == 0 {
		return 0x80004005
	}
	vtable := *(*uintptr)(unsafe.Pointer(mfPlayer))
	fn := *(*uintptr)(unsafe.Pointer(vtable + uintptr(index)*unsafe.Sizeof(uintptr(0))))
	all := append([]uintptr{mfPlayer}, args...)
	result, _, _ := syscall.SyscallN(fn, all...)
	return result
}
func startMFVideo(path string) bool {
	create := mfDLL.NewProc("MFPCreateMediaPlayer")
	if create.Find() != nil {
		return false
	}
	hr := call(coInitialize, 0, 2)
	mfCOM = int32(hr) >= 0
	mfHost = call(createWindow, 0, uintptr(unsafe.Pointer(wp("STATIC"))), 0, 0x50000000|0x02000000, 0, 0, 640, 360, mediaWindow, 0, call(kernel.NewProc("GetModuleHandleW"), 0), 0)
	if mfHost == 0 {
		return false
	}
	hr = call(create, uintptr(unsafe.Pointer(wp(path))), 1, 0, 0, mfHost, uintptr(unsafe.Pointer(&mfPlayer)))
	if int32(hr) < 0 || mfPlayer == 0 {
		destroyWindow.Call(mfHost)
		mfHost = 0
		return false
	}
	mfPaused = false
	return true
}
func closeMFVideo() {
	if mfPlayer != 0 {
		mfCall(38)
		mfCall(2)
		mfPlayer = 0
	}
	mfHost = 0
	if mfCOM {
		coUninitialize.Call()
		mfCOM = false
	}
}

func loadChatImage(path string) mediaImage {
	stat, err := os.Stat(path)
	if err != nil || stat.Size() > 128*1024*1024 {
		return mediaImage{}
	}
	if c, ok := chatMediaImages[path]; ok && c.size == stat.Size() && c.mod == stat.ModTime().UnixNano() {
		return c
	}
	if c := chatMediaImages[path]; c.image != 0 {
		call(vector.NewProc("GdipDisposeImage"), c.image)
		delete(chatMediaImages, path)
	}
	if len(chatMediaImages) >= 24 {
		for k, c := range chatMediaImages {
			call(vector.NewProc("GdipDisposeImage"), c.image)
			delete(chatMediaImages, k)
		}
	}
	c := mediaImage{size: stat.Size(), mod: stat.ModTime().UnixNano()}
	if call(vector.NewProc("GdipLoadImageFromFile"), uintptr(unsafe.Pointer(wp(path))), uintptr(unsafe.Pointer(&c.image))) != 0 {
		return mediaImage{}
	}
	call(vector.NewProc("GdipGetImageWidth"), c.image, uintptr(unsafe.Pointer(&c.width)))
	call(vector.NewProc("GdipGetImageHeight"), c.image, uintptr(unsafe.Pointer(&c.height)))
	if c.width == 0 || c.height == 0 || uint64(c.width)*uint64(c.height) > 40000000 {
		call(vector.NewProc("GdipDisposeImage"), c.image)
		return mediaImage{}
	}
	chatMediaImages[path] = c
	return c
}
func releaseChatMedia() {
	for k, c := range chatMediaImages {
		call(vector.NewProc("GdipDisposeImage"), c.image)
		delete(chatMediaImages, k)
	}
}
func drawChatImage(dc uintptr, c mediaImage, r rect) bool {
	if c.image == 0 {
		return false
	}
	w, h := int(r.R-r.L), int(r.B-r.T)
	if w <= 0 || h <= 0 {
		return false
	}
	fit := float64(w) / float64(c.width)
	if v := float64(h) / float64(c.height); v < fit {
		fit = v
	}
	iw, ih := int(float64(c.width)*fit), int(float64(c.height)*fit)
	x, y := int(r.L)+(w-iw)/2, int(r.T)+(h-ih)/2
	g := graphics(dc)
	if g == 0 {
		return false
	}
	defer call(vector.NewProc("GdipDeleteGraphics"), g)
	call(vector.NewProc("GdipSetInterpolationMode"), g, 7)
	return call(vector.NewProc("GdipDrawImageRect"), g, c.image, real(float32(x)), real(float32(y)), real(float32(iw)), real(float32(ih))) == 0
}
func isChatMedia(t Task) bool { k := taskMediaKind(t); return k == "image" || k == "video" }
func chatMediaReady(t Task) bool {
	return t.Direction == "out" && t.Status != "preparing" || t.Direction == "in" && t.Status == "done"
}
func paintChatMedia(dc uintptr, t Task, b chatBubble) bool {
	if !isChatMedia(t) || !chatMediaReady(t) {
		return false
	}
	path, kind, err := guiNode.localAttachment(t.ID)
	if err != nil {
		return false
	}
	saved := call(gdi.NewProc("SaveDC"), dc)
	call(gdi.NewProc("IntersectClipRect"), dc, uintptr(sc(b.X)), uintptr(sc(b.Y)), uintptr(sc(b.X+b.W)), uintptr(sc(b.Y+b.H)))
	defer call(gdi.NewProc("RestoreDC"), dc, saved)
	b.Y -= b.MediaOffset
	b.H = 180
	area := rct(b.X+8, b.Y+7, b.W-16, b.H-34)
	fill(dc, area, color(235, 240, 231))
	if kind == "image" {
		if !drawChatImage(dc, loadChatImage(path), area) {
			centerText(dc, "图片 · 点击查看", area, accent, guiSmall)
		}
	} else {
		centerText(dc, "▶", area, accent, fontSize(29, 600))
		label(dc, "视频", b.X+12, b.Y+8, b.W-24, 18, accent, guiTiny)
	}
	label(dc, t.Title, b.X+12, b.Y+b.H-25, b.W-24, 19, ink, guiSmall)
	return true
}
func openWithSystem(path string) {
	result := call(shell.NewProc("ShellExecuteW"), guiMain, uintptr(unsafe.Pointer(wp("open"))), uintptr(unsafe.Pointer(wp(path))), 0, 0, 1)
	if result <= 32 {
		showError("无法打开文件，请先安装支持此格式的查看器。")
	}
}
func openChatAttachment(t Task) {
	if t.Folder || taskMediaKind(t) == "files" {
		guiNode.mu.Lock()
		if full := guiNode.cfg.Tasks[t.ID]; full != nil {
			t = *full
			t.Files = append([]Meta(nil), full.Files...)
		}
		guiNode.mu.Unlock()
		path := t.Root
		if t.Direction == "out" && len(t.Files) > 0 {
			path = filepath.Dir(t.Files[0].Source)
		}
		if path != "" {
			openWithSystem(path)
		}
		return
	}
	path, kind, err := guiNode.localAttachment(t.ID)
	if err != nil {
		showError(err.Error())
		return
	}
	if kind != "image" && kind != "video" {
		openWithSystem(path)
		return
	}
	showMediaWindow(path, kind)
}
func videoCommand(command string) (string, error) {
	buf := make([]uint16, 512)
	code := call(videoDLL.NewProc("mciSendStringW"), uintptr(unsafe.Pointer(wp(command))), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), mediaWindow)
	if code != 0 {
		call(videoDLL.NewProc("mciGetErrorStringW"), code, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		return "", fmt.Errorf("%s", syscall.UTF16ToString(buf))
	}
	return syscall.UTF16ToString(buf), nil
}
func showMediaWindow(path, kind string) {
	if mediaWindow != 0 {
		destroyWindow.Call(mediaWindow)
	}
	mediaPath, mediaType, mediaError = path, kind, ""
	instance := call(kernel.NewProc("GetModuleHandleW"), 0)
	wc := winClass{Size: uint32(unsafe.Sizeof(winClass{})), Proc: syscall.NewCallback(mediaWindowProc), Instance: instance, Icon: guiAppIcon, SmallIcon: guiSmallIcon, Cursor: call(user.NewProc("LoadCursorW"), 0, 32512), Background: guiWhiteBrush, Name: wp("LinNodeMedia")}
	call(user.NewProc("RegisterClassExW"), uintptr(unsafe.Pointer(&wc)))
	mediaWindow = call(createWindow, 0, uintptr(unsafe.Pointer(wc.Name)), uintptr(unsafe.Pointer(wp("邻传 · "+filepath.Base(path)))), 0x00cf0000|0x02000000, 100, 80, uintptr(sc(840)), uintptr(sc(580)), guiMain, 0, instance, 0)
	if mediaWindow == 0 {
		showError("无法打开预览窗口")
		return
	}
	setDesktopWindowIcons(mediaWindow)
	if kind == "video" && !startMFVideo(path) {
		_, err := videoCommand(fmt.Sprintf("open %q type mpegvideo alias lindropvideo parent %d style child wait", path, mediaWindow))
		if err != nil {
			_, err = videoCommand(fmt.Sprintf("open %q alias lindropvideo parent %d style child wait", path, mediaWindow))
		}
		if err != nil {
			mediaError = "此系统无法播放该视频，可使用系统播放器打开。"
		} else {
			videoOpened = true
			videoCommand("set lindropvideo time format milliseconds")
			videoCommand("play lindropvideo")
		}
	}
	labels := []string{"关闭", "系统打开"}
	if kind == "video" {
		labels = []string{"暂停 / 继续", "重新播放", "系统播放器", "关闭"}
	}
	for i, label := range labels {
		h := call(createWindow, 0, uintptr(unsafe.Pointer(wp("BUTTON"))), uintptr(unsafe.Pointer(wp(label))), 0x50010000, 0, 0, 110, 30, mediaWindow, uintptr(180+i), instance, 0)
		msg(h, 0x30, guiSmall, 1)
		mediaButtons = append(mediaButtons, h)
	}
	layoutMedia()
	showWindow.Call(mediaWindow, 1)
	call(user.NewProc("SetForegroundWindow"), mediaWindow)
	// Static images repaint only on exposure/resize. A video status timer must
	// never turn an image viewer into a continuously clearing animation loop.
	if kind == "video" {
		call(user.NewProc("SetTimer"), mediaWindow, 1, 500, 0)
	}
}
func layoutMedia() {
	var r rect
	call(user.NewProc("GetClientRect"), mediaWindow, uintptr(unsafe.Pointer(&r)))
	w, h := int(r.R), int(r.B)
	for i, b := range mediaButtons {
		call(user.NewProc("MoveWindow"), b, uintptr(sc(16+i*118)), uintptr(h-sc(44)), uintptr(sc(110)), uintptr(sc(30)), 1)
	}
	if mfPlayer != 0 {
		call(user.NewProc("MoveWindow"), mfHost, 0, 0, uintptr(w), uintptr(h-sc(88)), 1)
		mfCall(32)
	}
	if videoOpened {
		videoCommand(fmt.Sprintf("put lindropvideo window at 0 0 %d %d", w, h-sc(88)))
		videoCommand(fmt.Sprintf("put lindropvideo destination at 0 0 %d %d", w, h-sc(88)))
	}
	invalidate(mediaWindow)
}
func mediaWindowProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case 0x14:
		return 1 // The offscreen frame includes the background.
	case 0x10:
		destroyWindow.Call(hwnd)
		return 0
	case 0x2:
		closeMFVideo()
		if videoOpened {
			videoCommand("close lindropvideo")
			videoOpened = false
		}
		call(user.NewProc("KillTimer"), hwnd, 1)
		mediaWindow = 0
		mediaButtons = nil
		return 0
	case 0x5:
		if mediaWindow != 0 {
			layoutMedia()
		}
		return 0
	case 0x100:
		if w == 27 {
			destroyWindow.Call(hwnd)
			return 0
		}
	case 0x111:
		id := int(w & 0xffff)
		if mediaType == "image" {
			if id == 180 {
				destroyWindow.Call(hwnd)
			}
			if id == 181 {
				openWithSystem(mediaPath)
			}
			return 0
		}
		switch id {
		case 180:
			if mfPlayer != 0 {
				var state uint32
				mfCall(13, uintptr(unsafe.Pointer(&state)))
				if state != 2 {
					mfCall(3)
				} else {
					mfCall(4)
				}
				mfPaused = !mfPaused
				return 0
			}
			mode, _ := videoCommand("status lindropvideo mode")
			if strings.TrimSpace(mode) == "playing" {
				videoCommand("pause lindropvideo")
			} else {
				videoCommand("play lindropvideo")
			}
		case 181:
			if mfPlayer != 0 {
				oldHost := mfHost
				closeMFVideo()
				if oldHost != 0 {
					destroyWindow.Call(oldHost)
				}
				if !startMFVideo(mediaPath) {
					mediaError = "无法重新播放，请使用系统播放器。"
				}
				layoutMedia()
				return 0
			}
			videoCommand("seek lindropvideo to start")
			videoCommand("play lindropvideo")
		case 182:
			openWithSystem(mediaPath)
		case 183:
			destroyWindow.Call(hwnd)
		}
		return 0
	case 0x113:
		if w == 1 && mediaType == "video" {
			var r rect
			call(user.NewProc("GetClientRect"), hwnd, uintptr(unsafe.Pointer(&r)))
			r.T = r.B - int32(sc(88))
			r.B -= int32(sc(60))
			if r.T < 0 {
				r.T = 0
			}
			if r.B > r.T {
				call(user.NewProc("InvalidateRect"), hwnd, uintptr(unsafe.Pointer(&r)), 0)
			}
		}
		return 0
	case 0xf:
		paintMediaWindow(hwnd)
		return 0
	}
	return call(user.NewProc("DefWindowProcW"), hwnd, uintptr(m), w, l)
}

func paintMediaWindow(hwnd uintptr) {
	var ps paintStruct
	dc := call(user.NewProc("BeginPaint"), hwnd, uintptr(unsafe.Pointer(&ps)))
	defer call(user.NewProc("EndPaint"), hwnd, uintptr(unsafe.Pointer(&ps)))
	var client rect
	call(user.NewProc("GetClientRect"), hwnd, uintptr(unsafe.Pointer(&client)))
	if client.R <= 0 || client.B <= 0 {
		return
	}
	bufferDC(dc, int(client.R), int(client.B), 0, 0, func(dc uintptr) {
		fill(dc, client, paper)
		r := client
		r.B -= int32(sc(60))
		if mediaType == "image" {
			if !drawChatImage(dc, loadChatImage(mediaPath), r) {
				centerText(dc, "此格式无法预览，请使用系统打开", r, muted, guiFont)
			}
		}
		if mediaError != "" {
			centerText(dc, mediaError, r, muted, guiFont)
		}
		if mfPlayer != 0 {
			var current uint32
			mfCall(13, uintptr(unsafe.Pointer(&current)))
			state := "正在播放"
			if current == 3 {
				state = "已暂停"
			} else if current != 2 {
				state = "播放已停止，可重新播放"
			}
			label(dc, state, 16, int(float64(r.B)/guiScale)-23, 350, 18, muted, guiSmall)
		}
		if videoOpened {
			pos, _ := videoCommand("status lindropvideo position")
			length, _ := videoCommand("status lindropvideo length")
			p, _ := strconv.Atoi(strings.TrimSpace(pos))
			n, _ := strconv.Atoi(strings.TrimSpace(length))
			label(dc, fmt.Sprintf("%02d:%02d / %02d:%02d", p/60000, p/1000%60, n/60000, n/1000%60), 16, int(float64(r.B)/guiScale)-23, 350, 18, muted, guiSmall)
		}
	})
	// A status tick never repaints the child video surface. Exposing or resizing
	// the actual player region still asks MFPlay to restore its image.
	if mfPlayer != 0 && ps.Rect.T < client.B-int32(sc(88)) {
		mfCall(32)
	}
}

func paintDocumentGlyph(dc uintptr, x, y int) {
	rounded(dc, rct(x, y, 23, 30), paper, lineColor, 3)
	stroke(dc, x+5, y+11, x+18, y+11, accent, 1)
	stroke(dc, x+5, y+17, x+18, y+17, accent, 1)
	stroke(dc, x+5, y+23, x+14, y+23, accent, 1)
}

func chatMediaParts(t Task) []chatTextPart {
	height := 180
	step := int(255/guiScale) - 37
	if step > height {
		step = height
	}
	if step < 1 {
		step = 1
	}
	var out []chatTextPart
	for offset := 0; offset < height; offset += step {
		n := step
		if height-offset < n {
			n = height - offset
		}
		out = append(out, chatTextPart{ID: t.ID, First: offset == 0, Last: offset+n == height, MediaOffset: offset, MediaHeight: n})
	}
	return out
}
