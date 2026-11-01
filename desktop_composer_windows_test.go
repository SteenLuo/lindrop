package main

import (
	"bytes"
	"image/png"
	"os"
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

func TestComposerScreenshotQueuePartialFailureAndTransfer(t *testing.T) {
	a, b := testNode(t, "截图发送端"), testNode(t, "截图接收端")
	connect(t, a, b)
	b.mu.Lock()
	b.cfg.Trust[a.id] = true
	b.mu.Unlock()
	img, _ := decodeScreenshotDIB(screenshotDIBFixture(24, false, 40))
	one, err := saveDraftScreenshot(a.dir, img)
	if err != nil {
		t.Fatal(err)
	}
	two, err := saveDraftScreenshot(a.dir, img)
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(two.Path)
	textID, sent, err := sendComposerContent(a, "截图说明😀", b.id, []draftPicture{one, two})
	if err == nil || textID == "" || len(sent) != 1 || sent[0] != one.ID {
		t.Fatal("lost successful prefix on partial failure", textID, sent, err)
	}
	pending := withoutDraftPicture([]draftPicture{one, two}, one.ID)
	if len(pending) != 1 || pending[0].ID != two.ID || !fileExists(one.Path) {
		t.Fatal("queued source deleted or failed draft lost")
	}
	waitFor(t, func() bool { return taskDone(a, textID) && taskDone(b, textID) })
	a.mu.Lock()
	var fileID string
	for id, task := range a.cfg.Tasks {
		if task.Kind != "text" {
			fileID = id
			if taskMediaKind(*task) != "image" {
				t.Error("screenshot is not an inline image")
			}
		}
	}
	a.mu.Unlock()
	if fileID == "" {
		t.Fatal("screenshot was not queued")
	}
	waitFor(t, func() bool { return taskDone(a, fileID) && taskDone(b, fileID) })
	b.mu.Lock()
	received := b.cfg.Tasks[fileID].Files[0].Final
	b.mu.Unlock()
	src, _ := os.ReadFile(one.Path)
	dst, err := os.ReadFile(received)
	if err != nil || hashBytes(src) != hashBytes(dst) {
		t.Fatal("received PNG bytes differ", err)
	}
	// Image-only sends must not manufacture an empty text message.
	_, sent, err = sendComposerContent(a, "  \r\n", b.id, []draftPicture{one})
	if err != nil || len(sent) != 1 {
		t.Fatal("image-only send failed", err)
	}
}

func TestComposerHiddenEditScrollPasteAndDeviceIsolation(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	oldMain, oldNode, oldControls, oldAll := guiMain, guiNode, guiControls, guiAllControls
	oldButtons, oldScale, oldFont := guiButtonProc, guiScale, guiFont
	oldPictures, oldPending, oldDraftDevice := chatPictures, chatPicturePending, chatDraftDevice
	oldDevices, oldSelected, oldRead, oldProc := guiDevices, guiSelected, chatReadScreenshot, chatComposeProc
	oldFeedback, oldFeedbackUntil := chatFeedback, chatFeedbackUntil
	oldWant, oldInLayout := guiWant, guiInLayout
	oldWidth, oldHeight := guiWidth, guiHeight
	defer func() {
		guiMain, guiNode, guiControls, guiAllControls = oldMain, oldNode, oldControls, oldAll
		guiButtonProc, guiScale, guiFont = oldButtons, oldScale, oldFont
		chatPictures, chatPicturePending, chatDraftDevice = oldPictures, oldPending, oldDraftDevice
		guiDevices, guiSelected, chatReadScreenshot, chatComposeProc = oldDevices, oldSelected, oldRead, oldProc
		chatFeedback, chatFeedbackUntil = oldFeedback, oldFeedbackUntil
		guiWant, guiInLayout = oldWant, oldInLayout
		guiWidth, guiHeight = oldWidth, oldHeight
	}()
	n, err := NewNode(t.TempDir(), "输入验证", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	guiNode = n
	guiControls = map[int]uintptr{}
	guiAllControls = nil
	guiButtonProc = map[uintptr]uintptr{}
	chatPictures = map[string][]draftPicture{}
	chatPicturePending = map[string]int{}
	guiMain = call(createWindow, 0, uintptr(unsafe.Pointer(wp("STATIC"))), 0, 0x80000000, 0, 0, 940, 650, 0, 0, 0, 0)
	if guiMain == 0 {
		t.Fatal("hidden parent creation")
	}
	defer destroyWindow.Call(guiMain)
	defer closeChatComposer()
	guiScale = 1
	guiWidth = 940
	guiHeight = 650
	guiFont = fontSize(13, 400)
	defer call(gdi.NewProc("DeleteObject"), guiFont)
	createChatComposer()
	edit := guiControls[170]
	call(user.NewProc("MoveWindow"), edit, 0, 0, 500, 96, 0)
	style := call(user.NewProc("GetWindowLongW"), edit, uintptr(0xfffffff0))
	if style&0x00200000 != 0 || style&4 == 0 || style&0x40 == 0 || guiControls[composerScrollbarID] == 0 {
		t.Fatal("input must use multiline editing with a separate client scrollbar")
	}
	long := strings.Repeat("中文长文字😀 with wrapping content\r\n", 120)
	set(edit, long)
	msg(edit, 0xb6, 0, 999) // EM_LINESCROLL, no real mouse or keyboard.
	if msg(edit, 0xce, 0, 0) == 0 || text(edit) != long {
		t.Fatal("native edit did not scroll or changed text")
	}
	checkComposerScrollbarNavigation(t, edit)
	set(edit, "")
	chatDraftDevice = "A"
	guiSelected = "A"
	guiDevices = []Device{{ID: "A", Name: "A", Online: true}, {ID: "B", Name: "B", Online: true}}
	raw := screenshotDIBFixture(32, true, 40)
	readCalls := 0
	chatReadScreenshot = func(uintptr) (clipboardScreenshot, bool, error) {
		readCalls++
		return clipboardScreenshot{Raw: raw}, true, nil
	}
	msg(edit, 0x302, 0, 0)
	if readCalls != 1 || chatPicturePending["A"] != 1 {
		t.Fatal("WM_PASTE did not prepare image")
	}
	// Switch before encoding completes. Completion must still belong to A.
	chatDraftDevice = "B"
	guiSelected = "B"
	chatPictureWorkers.Wait()
	receivePreparedScreenshots()
	if len(chatPictures["A"]) != 1 || len(chatPictures["B"]) != 0 || text(edit) != "" {
		t.Fatal("async screenshot crossed devices or entered edit as text")
	}
	p := chatPictures["A"][0]
	if !fileExists(p.Path) {
		t.Fatal("PNG source was not saved")
	}
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		var original [256]byte
		call(user.NewProc("GetKeyboardState"), uintptr(unsafe.Pointer(&original[0])))
		defer call(user.NewProc("SetKeyboardState"), uintptr(unsafe.Pointer(&original[0])))
		// SetKeyboardState changes this disposable runner thread's state;
		// no SendInput, keybd_event or actual keyboard automation is used.
		for _, key := range []struct {
			modifier  int
			key       uintptr
			character uintptr
		}{{0x11, 'V', 22}, {0x10, 0x2d, 0}} {
			state := original
			state[key.modifier] = 0x80
			call(user.NewProc("SetKeyboardState"), uintptr(unsafe.Pointer(&state[0])))
			before := readCalls
			msg(edit, 0x100, key.key, 0)
			if key.character != 0 {
				msg(edit, 0x102, key.character, 0)
			}
			if readCalls != before+1 {
				t.Fatal("paste shortcut missing or duplicated", key, readCalls, before)
			}
			chatDraftDevice = "not-visible"
			chatPictureWorkers.Wait()
			receivePreparedScreenshots()
			chatDraftDevice = "B"
		}
		call(user.NewProc("SetKeyboardState"), uintptr(unsafe.Pointer(&original[0])))
	}
	// Prove plain paste dispatch uses the native EDIT path without touching
	// the user's actual clipboard: the fake reports no image, procedure returns.
	chatReadScreenshot = func(uintptr) (clipboardScreenshot, bool, error) {
		readCalls++
		return clipboardScreenshot{}, false, nil
	}
	if pasteChatScreenshot() {
		t.Fatal("ordinary text paste swallowed")
	}
	if call(user.NewProc("IsWindowVisible"), guiMain) != 0 {
		t.Fatal("test exposed a foreground window")
	}
	// All thumbnails must fit even in the minimum supported window.
	for _, scale := range []float64{1, 2} {
		guiScale = scale
		for i := 0; i < 8; i++ {
			r := pictureDraftRect(i, 8)
			if r.L < int32(sc(343)) || r.R > int32(sc(guiWidth-27)) {
				t.Fatal("thumbnail overflow")
			}
		}
	}
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		// Capture belongs only to a disposable remote desktop. Never change
		// capture on the user's working computer, even for a hidden window.
		oldPage, oldPositions, oldHeight, oldTasks := guiPage, guiPositions, chatComposerHeight, guiTasks
		oldLayoutWidth, oldHover, oldResize := chatLayoutWidth, guiHover, chatResizeActive
		defer func() {
			guiPage, guiPositions, chatComposerHeight, guiTasks = oldPage, oldPositions, oldHeight, oldTasks
			chatLayoutWidth, guiHover, chatResizeActive = oldLayoutWidth, oldHover, oldResize
		}()
		guiScale = 1
		guiPage = 0
		guiPositions = map[int][4]int{}
		guiTasks = nil
		chatLayoutWidth = chatListWidth()
		g := currentComposerLayout()
		coord := func(x, y int) uintptr { return uintptr(uint16(x)) | uintptr(uint16(y))<<16 }
		if !composerMouse(guiMain, 0x201, 1, coord(700, g.Top+2)) || !chatResizeActive {
			t.Fatal("resize did not capture separator")
		}
		composerMouse(guiMain, 0x200, 1, coord(700, g.Top-78))
		flushComposerResize()
		if currentComposerLayout().TextHeight != g.TextHeight+80 {
			t.Fatal("drag did not enlarge input")
		}
		composerMouse(guiMain, 0x200, 1, coord(700, 1000))
		flushComposerResize()
		if currentComposerLayout().TextHeight != 64 {
			t.Fatal("downward drag did not clamp at minimum")
		}
		composerMouse(guiMain, 0x202, 0, coord(700, 1000))
		if chatResizeActive || call(user.NewProc("GetCapture")) == guiMain {
			t.Fatal("mouse capture leaked")
		}
	}
}

// Only the disposable remote Windows runner may write its clipboard. Local
// runs skip this test, including when running the rest of the hidden tests.
func TestClipboardScreenshotNativeFormatsOnRunner(t *testing.T) {
	if os.Getenv("LINDROP_TEST_CLIPBOARD") != "1" || os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("remote clipboard fixture only")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	owner := call(createWindow, 0, uintptr(unsafe.Pointer(wp("STATIC"))), 0, 0x80000000, 0, 0, 100, 100, 0, 0, 0, 0)
	if owner == 0 {
		t.Fatal("clipboard owner creation")
	}
	defer destroyWindow.Call(owner)
	dib := screenshotDIBFixture(24, false, 40)
	img, _ := decodeScreenshotDIB(dib)
	var encoded bytes.Buffer
	png.Encode(&encoded, img)
	pngFormat := uint32(call(user.NewProc("RegisterClipboardFormatW"), uintptr(unsafe.Pointer(wp("PNG")))))
	defer func() {
		if call(user.NewProc("OpenClipboard"), owner) != 0 {
			call(user.NewProc("EmptyClipboard"))
			call(user.NewProc("CloseClipboard"))
		}
	}()
	for _, sample := range []struct {
		format uint32
		raw    []byte
	}{{8, dib}, {17, screenshotDIBFixture(32, true, 124)}, {pngFormat, encoded.Bytes()}} {
		if call(user.NewProc("OpenClipboard"), owner) == 0 {
			t.Fatal("open runner clipboard")
		}
		call(user.NewProc("EmptyClipboard"))
		h := call(kernel.NewProc("GlobalAlloc"), 2, uintptr(len(sample.raw)))
		ptr := call(kernel.NewProc("GlobalLock"), h)
		if ptr == 0 {
			call(user.NewProc("CloseClipboard"))
			t.Fatal("clipboard allocation")
		}
		copy(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), len(sample.raw)), sample.raw)
		call(kernel.NewProc("GlobalUnlock"), h)
		if call(user.NewProc("SetClipboardData"), uintptr(sample.format), h) == 0 {
			call(kernel.NewProc("GlobalFree"), h)
			call(user.NewProc("CloseClipboard"))
			t.Fatal("populate runner clipboard")
		}
		call(user.NewProc("CloseClipboard"))
		shot, available, err := readClipboardScreenshot(owner)
		if err != nil || !available {
			t.Fatal("native clipboard read failed", sample.format, err)
		}
		if shot.PNG {
			img, err = decodeScreenshotPNG(shot.Raw)
		} else {
			img, err = decodeScreenshotDIB(shot.Raw)
		}
		if err != nil || img.Bounds().Dx() != 3 || img.Bounds().Dy() != 2 {
			t.Fatal("native clipboard pixels failed", sample.format, err)
		}
	}
}
