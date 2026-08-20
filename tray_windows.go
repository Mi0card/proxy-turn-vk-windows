//go:build windows

package main

import (
	"bytes"
	"errors"
	"image/png"
	"runtime"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// ── Win32 API ────────────────────────────────────────────────────────────────

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procCreateIconIndirect  = user32.NewProc("CreateIconIndirect")
	procDestroyIcon         = user32.NewProc("DestroyIcon")
	procLoadImageW          = user32.NewProc("LoadImageW")
	procCreateBitmap        = gdi32.NewProc("CreateBitmap")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procShellNotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
)

const (
	wmUser          = 0x0400
	wmTrayIcon      = wmUser + 1
	wmCommand       = 0x0111
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205

	menuShow = 1001
	menuQuit = 1002

	mfString    = 0x00000000
	mfSeparator = 0x00000800
	mfGrayed    = 0x00000001

	tpmLeftAlign   = 0x0000
	tpmBottomAlign = 0x0002
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	imageIcon      = 1
	lrDefaultColor = 0x00000000
	iconSmall      = 0

	notifyIconDataV2Size = 952
)

type point struct{ X, Y int32 }

type msg struct {
	hwnd    syscall.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type iconInfo struct {
	FIcon    int32
	XHotspot int32
	YHotspot int32
	HbmMask  syscall.Handle
	HbmColor syscall.Handle
}

type notifyIconData struct {
	cbSize           uint32
	hWnd             syscall.Handle
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            syscall.Handle
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     syscall.Handle
}

// ── Состояние трея ───────────────────────────────────────────────────────────

var (
	trayMu     sync.Mutex
	trayApp    *App
	trayHWND   syscall.Handle
	trayHICON  syscall.Handle
	trayActive bool
)

// ── Иконка ───────────────────────────────────────────────────────────────────

// trayCreateHICON собирает HICON из PNG-кадра embedded-иконки.
func trayCreateHICON() (syscall.Handle, error) {
	pngData, err := trayIconPNG()
	if err != nil {
		return 0, err
	}
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return 0, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return 0, errors.New("пустое изображение")
	}
	// CreateBitmap для 32bpp ожидает порядок B,G,R,A сверху вниз.
	rgba := make([]byte, w*h*4)
	i := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bb, aa := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			rgba[i] = byte(bb >> 8)
			rgba[i+1] = byte(g >> 8)
			rgba[i+2] = byte(r >> 8)
			rgba[i+3] = byte(aa >> 8)
			i += 4
		}
	}

	hbmColor, _, _ := procCreateBitmap.Call(uintptr(w), uintptr(h), 1, 32, uintptr(unsafe.Pointer(&rgba[0])))
	if hbmColor == 0 {
		return 0, errors.New("CreateBitmap: ошибка")
	}
	defer procDeleteObject.Call(hbmColor)
	hbmMask, _, _ := procCreateBitmap.Call(uintptr(w), uintptr(h), 1, 1, 0)
	if hbmMask == 0 {
		return 0, errors.New("CreateBitmap (mask): ошибка")
	}
	defer procDeleteObject.Call(hbmMask)

	ii := &iconInfo{FIcon: 1, HbmMask: syscall.Handle(hbmMask), HbmColor: syscall.Handle(hbmColor)}
	hIcon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(ii)))
	if hIcon == 0 {
		return 0, errors.New("CreateIconIndirect: ошибка")
	}
	return syscall.Handle(hIcon), nil
}

// trayLoadExeIcon — fallback: иконка из ресурсов самого exe (ID 1).
func trayLoadExeIcon() syscall.Handle {
	hInst, _, _ := procGetModuleHandleW.Call(0)
	hIcon, _, _ := procLoadImageW.Call(hInst, 1, imageIcon, 16, 16, lrDefaultColor)
	return syscall.Handle(hIcon)
}

// ── Вспомогательные ──────────────────────────────────────────────────────────

func trayTip() string {
	if trayApp != nil {
		return "WinDTT — " + trayApp.trayStatusText()
	}
	return "WinDTT"
}

// traySetTip обновляет tooltip иконки через NIM_MODIFY.
func traySetTip() {
	trayMu.Lock()
	hwnd := trayHWND
	trayMu.Unlock()
	if hwnd == 0 {
		return
	}
	nid := notifyIconData{}
	nid.cbSize = notifyIconDataV2Size
	nid.hWnd = hwnd
	nid.uID = 1
	nid.uFlags = nifTip
	copyUTF16(nid.szTip[:], trayTip())
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

func copyUTF16(dst []uint16, s string) {
	for i, c := range utf16.Encode([]rune(s)) {
		if i >= len(dst)-1 {
			break
		}
		dst[i] = c
	}
}

func trayGetApp() *App {
	trayMu.Lock()
	defer trayMu.Unlock()
	return trayApp
}

// ── Окно и message loop ──────────────────────────────────────────────────────

func trayWndProc(hwnd syscall.Handle, m uint32, wParam, lParam uintptr) uintptr {
	switch m {
	case wmTrayIcon:
		switch lParam {
		case wmLButtonUp, wmLButtonDblClk:
			trayShowAction()
		case wmRButtonUp:
			trayShowMenu(hwnd)
		}
	case wmCommand:
		switch wParam & 0xffff {
		case menuShow:
			trayShowAction()
		case menuQuit:
			trayQuitAction()
		}
	case wmDestroy:
		procPostQuitMessage.Call(0)
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(m), wParam, lParam)
	return r
}

func trayShowMenu(hwnd syscall.Handle) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	// Строка статуса (серая, некликабельная).
	if trayApp != nil {
		statusPtr, _ := syscall.UTF16PtrFromString(trayApp.trayStatusText())
		procAppendMenuW.Call(menu, mfGrayed, 0, uintptr(unsafe.Pointer(statusPtr)))
	}
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)

	showPtr, _ := syscall.UTF16PtrFromString("Показать окно")
	procAppendMenuW.Call(menu, mfString, menuShow, uintptr(unsafe.Pointer(showPtr)))
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	quitPtr, _ := syscall.UTF16PtrFromString("Выход")
	procAppendMenuW.Call(menu, mfString, menuQuit, uintptr(unsafe.Pointer(quitPtr)))

	// Стандартный трюк для корректного закрытия меню по клику мимо.
	procSetForegroundWindow.Call(uintptr(hwnd))
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	cmd, _, _ := procTrackPopupMenu.Call(
		menu, tpmLeftAlign|tpmBottomAlign|tpmRightButton|tpmReturnCmd,
		uintptr(pt.X), uintptr(pt.Y), 0, uintptr(hwnd), 0)
	procPostMessageW.Call(uintptr(hwnd), 0, 0, 0)

	switch cmd {
	case menuShow:
		trayShowAction()
	case menuQuit:
		trayQuitAction()
	}
}

// trayRun создаёт скрытое окно, добавляет иконку и крутит message loop.
// Должен выполняться на отдельном OS-потоке: окно и его очередь сообщений
// привязаны к потоку создания.
func trayRun(hicon syscall.Handle, ready chan struct{}) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("WinDTTTrayWnd")

	wc := wndClassEx{}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	wc.lpfnWndProc = syscall.NewCallback(trayWndProc)
	wc.hInstance = syscall.Handle(hInst)
	wc.lpszClassName = className
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	wndName, _ := syscall.UTF16PtrFromString("WinDTT tray")
	hwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(wndName)),
		0, 0, 0, 0, 0,
		0, 0, hInst, 0)
	if hwnd == 0 {
		close(ready)
		return
	}

	trayMu.Lock()
	trayHWND = syscall.Handle(hwnd)
	trayHICON = hicon
	trayActive = true
	trayMu.Unlock()

	nid := notifyIconData{}
	nid.cbSize = notifyIconDataV2Size
	nid.hWnd = syscall.Handle(hwnd)
	nid.uID = 1
	nid.uFlags = nifMessage | nifIcon | nifTip
	nid.uCallbackMessage = wmTrayIcon
	nid.hIcon = hicon
	copyUTF16(nid.szTip[:], trayTip())
	procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	close(ready)

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if ret == 0 || ret == ^uintptr(0) {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// ── Публичный API (общий для всех платформ) ──────────────────────────────────

// trayInit создаёт иконку в трее. Вызывается из startup.
func trayInit(a *App) {
	trayMu.Lock()
	trayApp = a
	trayMu.Unlock()

	hicon, err := trayCreateHICON()
	if err != nil {
		hicon = trayLoadExeIcon()
	}
	if hicon == 0 {
		return // трей недоступен — продолжаем без него
	}
	ready := make(chan struct{})
	go trayRun(hicon, ready)
	<-ready
}

// trayRemove убирает иконку и останавливает message loop. Вызывается из shutdown.
func trayRemove(a *App) {
	trayMu.Lock()
	hwnd := trayHWND
	icon := trayHICON
	trayActive = false
	trayHWND = 0
	trayHICON = 0
	trayMu.Unlock()

	if hwnd == 0 {
		return
	}
	nid := notifyIconData{}
	nid.cbSize = notifyIconDataV2Size
	nid.hWnd = hwnd
	nid.uID = 1
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	if icon != 0 {
		procDestroyIcon.Call(uintptr(icon))
	}
	procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0)
}

// trayUpdateStatus обновляет tooltip иконки и статус в меню.
func trayUpdateStatus(a *App) {
	traySetTip()
}

// trayActivateApp выводит окно приложения на передний план. Само окно
// создаёт Wails, отдельного HWND здесь нет — ShowWindow() из Wails уже
// поднимает окно наверх, поэтому ничего делать не требуется.
func trayActivateApp() {}
