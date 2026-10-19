package main

import "fmt"

// rsrc assigns the icon group ID 2; ID 1 is the manifest, not an icon group.
const desktopIconResource = 2

var guiAppIcon, guiSmallIcon uintptr

func loadDesktopIcon(instance uintptr, width, height int) uintptr {
	// IMAGE_ICON, LR_SHARED: Windows owns these cached resource handles.
	return call(user.NewProc("LoadImageW"), instance, desktopIconResource, 1, uintptr(width), uintptr(height), 0x8000)
}

func loadDesktopIcons(instance uintptr) error {
	guiAppIcon = loadDesktopIcon(instance, int(call(user.NewProc("GetSystemMetrics"), 11)), int(call(user.NewProc("GetSystemMetrics"), 12)))
	guiSmallIcon = loadDesktopIcon(instance, int(call(user.NewProc("GetSystemMetrics"), 49)), int(call(user.NewProc("GetSystemMetrics"), 50)))
	if guiAppIcon == 0 || guiSmallIcon == 0 {
		return fmt.Errorf("无法加载内嵌应用图标（资源 %d）", desktopIconResource)
	}
	return nil
}

func setDesktopWindowIcons(hwnd uintptr) {
	msg(hwnd, 0x80, 1, guiAppIcon)   // WM_SETICON, ICON_BIG: taskbar / Alt+Tab.
	msg(hwnd, 0x80, 0, guiSmallIcon) // ICON_SMALL: title bar / system menu.
}
