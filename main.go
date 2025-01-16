package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Add this helper function for MAKEINTRESOURCE
func MAKEINTRESOURCE(id uint16) *uint16 {
	return (*uint16)(unsafe.Pointer(uintptr(id)))
}

const (
	WM_USER         = 0x0400
	WM_TRAYICON     = WM_USER + 1
	WM_COMMAND      = 0x0111
	WM_RBUTTONDOWN  = 0x0204
	WM_MOUSEMOVE    = 0x0200
	WM_LBUTTONDOWN  = 0x0201
	NIM_ADD         = 0x00000000
	NIM_MODIFY      = 0x00000001
	NIM_DELETE      = 0x00000002
	NIF_ICON        = 0x00000002
	NIF_MESSAGE     = 0x00000001
	NIF_TIP         = 0x00000004
	IDI_APPLICATION = 32512
	IDM_EXIT        = 0x0001
	IDM_OPEN_UI     = 0x0002
	WM_ENDSESSION   = 0x0016

	// Additional Windows constants
	WM_DESTROY   = 0x0002
	WM_CLOSE     = 0x0010
	WM_NULL      = 0x0000
	WM_RBUTTONUP = 0x0205
	WM_LBUTTONUP = 0x0202

	// Window Style constants
	WS_OVERLAPPEDWINDOW = 0x00CF0000

	// Class Style constants
	CS_HREDRAW = 0x0001
	CS_VREDRAW = 0x0002

	// System colors
	COLOR_WINDOW = 5

	// Cursor constants
	IDC_ARROW = 32512

	// Default position constants
	CW_USEDEFAULT = ^0x7fffffff

	NIF_STATE      = 0x00000008
	NIF_INFO       = 0x00000010
	NIIF_NONE      = 0x00000000
	NIS_HIDDEN     = 0x00000001
	NIS_SHAREDICON = 0x00000002

	SW_HIDE = 0
)

var (
	modKernel32 = windows.NewLazySystemDLL("Kernel32.dll")
	modShell32  = windows.NewLazySystemDLL("shell32.dll")
	modUser32   = windows.NewLazySystemDLL("user32.dll")

	procShellNotifyIcon     = modShell32.NewProc("Shell_NotifyIconW")
	procDestroyWindow       = modUser32.NewProc("DestroyWindow")
	procGetMessage          = modUser32.NewProc("GetMessageW")
	procCreateWindowEx      = modUser32.NewProc("CreateWindowExW")
	procRegisterClass       = modUser32.NewProc("RegisterClassW")
	procLoadIcon            = modUser32.NewProc("LoadIconW")
	procGetModuleHandle     = modKernel32.NewProc("GetModuleHandleW")
	procLoadCursor          = modUser32.NewProc("LoadCursorW")
	procDefWindowProc       = modUser32.NewProc("DefWindowProcW")
	procGetCursorPos        = modUser32.NewProc("GetCursorPos")
	procCreatePopupMenu     = modUser32.NewProc("CreatePopupMenu")
	procDestroyMenu         = modUser32.NewProc("DestroyMenu")
	procAppendMenu          = modUser32.NewProc("AppendMenuW")
	procSetForegroundWindow = modUser32.NewProc("SetForegroundWindow")
	procShowWindow          = modUser32.NewProc("ShowWindow")
	procTrackPopupMenu      = modUser32.NewProc("TrackPopupMenu")
	procPostMessage         = modUser32.NewProc("PostMessageW")
	procPostQuitMessage     = modUser32.NewProc("PostQuitMessage")
	procTranslateMessage    = modUser32.NewProc("TranslateMessage")
	procDispatchMessage     = modUser32.NewProc("DispatchMessageW")
	hInst                   windows.Handle
	hWnd                    windows.HWND
	mu                      sync.Mutex
	commands                chan string
)

// POINT represents a Windows POINT structure
type POINT struct {
	X, Y int32
}

type winMsg struct {
	Hwnd    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

// WindowClass represents the WNDCLASS structure
type WindowClass struct {
	Style      uint32
	WndProc    uintptr
	CbClsExtra int32
	CbWndExtra int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
}

type notifyIconData struct {
	CbSize           uint32
	HWnd             windows.HWND
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            windows.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16 // Notification message
	UVersion         uint32
	SzInfoTitle      [64]uint16 // Notification title
	DwInfoFlags      uint32     // Notification flags
	GuidItem         windows.GUID
	HBalloonIcon     windows.Handle
}

func TranslateMessage(msg *winMsg) {
	fmt.Println("TranslateMessage function")
	procTranslateMessage.Call(uintptr(unsafe.Pointer(msg)))
}

func DispatchMessage(msg *winMsg) {
	fmt.Println("DispatchMessage function")
	procDispatchMessage.Call(uintptr(unsafe.Pointer(msg)))
}

func postMessage(hwnd windows.HWND, msg uint32, wParam, lParam uintptr) bool {
	ret, _, _ := procPostMessage.Call(
		uintptr(hwnd),
		uintptr(msg),
		wParam,
		lParam)

	return ret != 0
}

func getMessage(msg *winMsg, hWnd windows.HWND, min, max uint32) int {
	ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(msg)), uintptr(hWnd), uintptr(min), uintptr(max))
	return int(ret)
}

func runMessageLoop() {
	msg := &winMsg{}
	for getMessage(msg, hWnd, 0, 0) > 0 {
		TranslateMessage(msg)
		DispatchMessage(msg)
	}
}

func main() {
	fmt.Println("About to call GetModuleHandle")
	// probably exists because no process is running
	handle, _, _ := procGetModuleHandle.Call(0)
	if handle == 0 {
		log.Fatal("GetModuleHandle failed: returned NULL")
	}
	hInst = windows.Handle(handle)

	className, err := windows.UTF16PtrFromString("SysTrayWindowClass")
	if err != nil {
		log.Fatal("UTF16PtrFromString: ", err)
	}

	wndClass := WindowClass{
		Style:      CS_HREDRAW | CS_VREDRAW,
		WndProc:    windows.NewCallback(wndProc), // pointer to callback function
		Instance:   hInst,
		Icon:       loadIcon(0, IDI_APPLICATION),
		Cursor:     loadCursor(0, IDC_ARROW),
		Background: windows.Handle(COLOR_WINDOW + 1),
		ClassName:  className,
	}

	if _, err = registerClass(&wndClass); err != nil {
		log.Fatal("RegisterClass: ", err)
	}

	// creates a hidden window
	hWnd, err = createWindowEx(
		0,
		className,
		nil,
		WS_OVERLAPPEDWINDOW,
		CW_USEDEFAULT,
		CW_USEDEFAULT,
		CW_USEDEFAULT,
		CW_USEDEFAULT,
		0,
		0,
		hInst,
		nil)
	if err != nil {
		log.Fatal("CreateWindowEx: ", err)
	}

	err = addTrayIcon(hWnd)
	if err != nil {
		log.Fatal("addTrayIcon: ", err)
	}

	// Display a test notification
	err = showNotification(hWnd, "Screen Time", "This is a test notification!")
	if err != nil {
		log.Fatal("showNotification: ", err)
	}
	//before we run we probably need a channel to collect commands from the menu actions

	commands = make(chan string, 10)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for {
			select {
			case cmd := <-commands:
				if cmd == "OpenUI" {
					fmt.Println("Command to open UI received")
					// cmd := exec.Command("cmd", "/c", "start", "http://localhost:5173")
					// if err := cmd.Start(); err != nil {
					// 	log.Printf("Error starting browser: %v", err)
					// }
				}
			case <-signals:
				//send window handle a message to close the message loop
				fmt.Println("Signal received")
				sucess := postMessage(hWnd, WM_CLOSE, 0, 0)

				if !sucess {
					fmt.Println("PostMessage to close window failed")
				}

				if hWnd != 0 {
					removeTrayIcon(hWnd)
					destroyWindow(hWnd)
					os.Exit(0)
				}
				return
			}
		}
	}()

	runMessageLoop()
}

func wndProc(hwnd windows.HWND, msg uint32, wParam, lParam uintptr) (lResult uintptr) {
	switch msg {
	case WM_DESTROY:
		fmt.Println("WM_DESTROY received")
		procPostQuitMessage.Call(uintptr(int32(0)))
		fallthrough
	case WM_ENDSESSION:
		fmt.Println("WM_ENDSESSION received")
		removeTrayIcon(hwnd)
	case WM_TRAYICON: // WM_USER + 1
		// is this breaking the application?
		switch lParam {
		case WM_MOUSEMOVE, WM_LBUTTONDOWN:
			// Do nothing
		case WM_RBUTTONUP:
			fmt.Println("Right button up received")
			showMenu()
		case 0x404:
			// Do nothing
		default:
			fmt.Printf("Unknown mouse event received: %d\n", lParam)
		}
	case WM_COMMAND:
		switch int32(wParam) {
		case IDM_OPEN_UI:
			fmt.Printf("opened the UI")
			select {
			case commands <- "OpenUI":
			// should not happen but in case not listening
			default:
				fmt.Println("no listener on OpenUI command")
			}
		case IDM_EXIT:
			log.Println("Exit clicked")
			postMessage(hwnd, WM_CLOSE, 0, 0)
		default:
			fmt.Println("Unknown command received")
		}
	case WM_CLOSE:
		fmt.Println("WM_CLOSE received")
		destroyWindow(hWnd)
	default:
		lResult = defWindowProc(hwnd, msg, wParam, lParam)
	}
	return
}

func addTrayIcon(hwnd windows.HWND) error {
	nid := notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:             hwnd,
		UID:              1,
		UFlags:           NIF_ICON | NIF_MESSAGE | NIF_TIP | NIF_STATE, // Added NIF_STATE
		UCallbackMessage: WM_TRAYICON,
		HIcon:            loadIcon(0, IDI_APPLICATION),
		DwState:          0,
		DwStateMask:      NIS_HIDDEN,
	}

	szTip, err := windows.UTF16FromString("Go SysTray")
	if err != nil {
		return err
	}
	copy(nid.SzTip[:], szTip)

	ret, _, err := procShellNotifyIcon.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid)))
	if ret == 0 {
		return fmt.Errorf("shell_notifyicon failed: %w", err)
	}

	// Modify after adding
	nid.UFlags = NIF_STATE
	nid.DwState = 0
	ret, _, err = procShellNotifyIcon.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&nid)))
	if ret == 0 {
		return fmt.Errorf("shell_notifyicon modify failed: %w", err)
	}

	return nil
}

var (
	// Add menu items
	openUIText, _ = windows.UTF16PtrFromString("Open UI")
	exitText, _   = windows.UTF16PtrFromString("Exit")
)

func showMenu() {
	// Menu flags
	const (
		MF_STRING       = 0x00000000
		MF_SEPARATOR    = 0x00000800
		TPM_RIGHTALIGN  = 0x0008
		TPM_BOTTOMALIGN = 0x0020
		TPM_RIGHTBUTTON = 0x0002
		TPM_NONOTIFY    = 0x0080
		TPM_RETURNCMD   = 0x0100
		TPM_LEFTALIGN   = 0x0000 //
	)

	// Create popup menu
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}

	procAppendMenu.Call(
		hMenu,
		uintptr(MF_STRING),
		uintptr(IDM_OPEN_UI),
		uintptr(unsafe.Pointer(openUIText)),
	)

	procAppendMenu.Call(
		hMenu,
		uintptr(MF_SEPARATOR),
		0,
		0,
	)

	procAppendMenu.Call(
		hMenu,
		uintptr(MF_STRING),
		uintptr(IDM_EXIT),
		uintptr(unsafe.Pointer(exitText)),
	)

	// Get cursor position
	pt := POINT{}
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	// introducing this makes it so the menu doesn't open again when the cursor is over it
	mu.Lock()
	boolRet, _, err := procSetForegroundWindow.Call(uintptr(hWnd))
	if boolRet == 0 {
		fmt.Println("SetForegroundWindow failed: ", err)
	}

	// Track popup menu - let Windows handle the command messages
	// this blocks and the program will not listen to clicks outside the menu to close it
	boolRet, _, err = procTrackPopupMenu.Call(
		hMenu,
		uintptr(TPM_BOTTOMALIGN|TPM_LEFTALIGN|TPM_RIGHTBUTTON),
		uintptr(pt.X),
		uintptr(pt.Y),
		0,
		uintptr(hWnd),
		0,
	)

	if boolRet == 0 {
		fmt.Println("TrackPopupMenu failed: ", err)
	}
	mu.Unlock()
	fmt.Println("TrackPopupMenu returned: ", boolRet)
	procDestroyMenu.Call(hMenu)
}

func removeTrayIcon(hwnd windows.HWND) error {
	nid := notifyIconData{
		CbSize: uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:   hwnd,
		UID:    1,
	}

	ret, _, err := procShellNotifyIcon.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
	if ret == 0 {
		return fmt.Errorf("shell_notifyicon failed: %w", err)
	}

	return nil
}

func loadIcon(hInst windows.Handle, resourceId int) windows.Handle {
	ret, _, err := procLoadIcon.Call(uintptr(hInst), uintptr(unsafe.Pointer(MAKEINTRESOURCE(uint16(resourceId)))))
	if ret == 0 {
		log.Println("LoadIcon failed: ", err)
		return 0
	}
	return windows.Handle(ret)
}

func loadCursor(hInst windows.Handle, resourceId int) windows.Handle {
	ret, _, err := procLoadCursor.Call(uintptr(hInst), uintptr(unsafe.Pointer(MAKEINTRESOURCE(uint16(resourceId)))))
	if ret == 0 {
		log.Println("LoadCursor failed: ", err)
		return 0
	}
	return windows.Handle(ret)
}

func registerClass(wndClass *WindowClass) (uint16, error) {
	ret, _, err := procRegisterClass.Call(uintptr(unsafe.Pointer(wndClass)))
	if ret == 0 {
		return 0, err
	}
	return uint16(ret), nil
}

func destroyWindow(hwnd windows.HWND) error {
	ret, _, err := procDestroyWindow.Call(uintptr(hwnd))
	if ret == 0 {
		return fmt.Errorf("DestroyWindow failed: %w", err)
	}
	return nil
}

func createWindowEx(exStyle uint32, className, windowName *uint16, style uint32, x, y, width, height int, parent, menu, instance windows.Handle, param unsafe.Pointer) (windows.HWND, error) {
	ret, _, err := procCreateWindowEx.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		uintptr(style),
		uintptr(x),
		uintptr(y),
		uintptr(width),
		uintptr(height),
		uintptr(parent),
		uintptr(menu),
		uintptr(instance),
		uintptr(param),
	)
	if ret == 0 {
		return 0, err
	}

	hwnd := windows.HWND(ret)

	// Hide the window but keep it active
	procShowWindow.Call(uintptr(hwnd), uintptr(SW_HIDE))

	return hwnd, nil
}

func defWindowProc(hwnd windows.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	fmt.Printf("DefWindowProc: %d\n", msg)
	ret, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}

func showNotification(hwnd windows.HWND, title, message string) error {
	nid := notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:             hwnd,
		UID:              1,
		UFlags:           NIF_INFO, // Use NIF_INFO for notifications
		UCallbackMessage: WM_TRAYICON,
		HIcon:            loadIcon(0, IDI_APPLICATION),
	}

	// Convert title and message to UTF-16
	titleUTF16, err := windows.UTF16FromString(title)
	if err != nil {
		return fmt.Errorf("failed to convert title to UTF-16: %w", err)
	}
	messageUTF16, err := windows.UTF16FromString(message)
	if err != nil {
		return fmt.Errorf("failed to convert message to UTF-16: %w", err)
	}

	// Copy title and message into the structure
	copy(nid.SzInfoTitle[:], titleUTF16)
	copy(nid.SzInfo[:], messageUTF16)

	// Set the notification flags (e.g., NIIF_NONE for no icon)
	nid.DwInfoFlags = NIIF_NONE

	// Call Shell_NotifyIcon with NIM_MODIFY to display the notification
	ret, _, err := procShellNotifyIcon.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&nid)))
	if ret == 0 {
		return fmt.Errorf("Shell_NotifyIcon failed: %w", err)
	}

	return nil
}
