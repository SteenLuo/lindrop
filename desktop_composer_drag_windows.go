package main

const composerDragTimer = 0x726

var chatDragFrame composerDragFrame

func requestComposerResize(height int) {
	if !chatResizeActive {
		return
	}
	if chatDragFrame.request(height) {
		if call(user.NewProc("SetTimer"), guiMain, composerDragTimer, 15, 0) == 0 {
			chatDragFrame.Armed = false
			flushComposerResize()
		}
	}
}

func flushComposerResize() {
	if height, ok := chatDragFrame.take(); ok {
		resizeChatComposer(height)
	}
}

func cancelComposerResize() {
	if chatDragFrame.Armed {
		call(user.NewProc("KillTimer"), guiMain, composerDragTimer)
	}
	chatDragFrame.reset()
	chatResizeActive = false
}

func finishComposerResize() {
	flushComposerResize()
	cancelComposerResize()
	// Refresh normal decorations once after the last precise pointer position,
	// rather than drawing the full main scene for every motion event.
	invalidate(guiMain)
}
