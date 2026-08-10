//go:build darwin

package main

import (
	"sync"

	"github.com/progrium/darwinkit/dispatch"
	"github.com/progrium/darwinkit/macos/appkit"
	"github.com/progrium/darwinkit/macos/foundation"
	"github.com/progrium/darwinkit/macos/webkit"
	"github.com/progrium/darwinkit/objc"
)

// captchaBridgeShimJS — мост JS -> Go для WKWebView. В отличие от WebView2
// (см. captcha_webview_windows.go), где webview2.Bind("name", fn) сам создаёт
// вызываемый нативный window.name(...), у WKWebView такого нет: единственный
// канал — window.webkit.messageHandlers.<name>.postMessage(value), который
// долетает до WKScriptMessageHandler в Go. Поэтому здесь сначала внедряется
// этот тонкий шим, переопределяющий window.wdttCaptchaSuccess/Error как обёртки
// над postMessage — а дальше общий captchaInterceptorJS (captcha_interceptor.go)
// работает не зная о разнице.
const captchaBridgeShimJS = `
window.wdttCaptchaSuccess = function(token) {
    window.webkit.messageHandlers.wdttCaptchaSuccess.postMessage(String(token));
};
window.wdttCaptchaError = function(err) {
    window.webkit.messageHandlers.wdttCaptchaError.postMessage(String(err));
};
`

// openCaptchaWebView — macOS-аналог captcha_webview_windows.go, через
// progrium/darwinkit (чистый Go биндинг AppKit/WebKit) вместо go-webview2
// (тот жёстко завязан на WebView2/COM, только Windows).
//
// НЕ ПРОВЕРЕНО СБОРКОЙ НА РЕАЛЬНОМ MAC — писалось без доступа к macOS,
// сигнатуры сверены построчно с исходником darwinkit v0.5.0 (webkit_custom.go,
// window.gen.go, window_delegate.gen.go, appkit_custom.go), но рантайм-
// поведение (особенно WindowWillClose и то, что postMessage реально доносит
// строку до message.Description()) нужно перепроверить на железе.
//
// Ключевое отличие от Windows-версии: там на каждый вызов поднимается
// отдельный OS-поток со своим Win32-message-loop (w.Run() блокирует именно
// его). На macOS так нельзя — NSApplication run loop на процесс один, и он
// уже занят Wails (main.go: wails.Run()). Второй вызов macos.RunApp() здесь
// создал бы второй run loop в одном процессе — поэтому вместо этого создание
// окна просто отправляется в существующий главный поток через
// dispatch.MainQueue().DispatchAsync, а вызывающая горутина блокируется на
// канале до результата — то же поведение снаружи (см. doc-комментарий в
// captcha_webview_windows.go), другая реализация внутри.
func openCaptchaWebView(redirectURI string, baseDir string, onResult func(result string)) {
	// baseDir на macOS не используется: WebView2 требует явный DataPath для
	// профиля (Windows), у WKWebView дефолтный WKWebsiteDataStore не требует
	// внешнего пути — если понадобится изоляция профиля, сюда добавляется
	// свой WKWebsiteDataStore через WebViewConfiguration.SetWebsiteDataStore.
	_ = baseDir

	var once sync.Once
	resultCh := make(chan string, 1)
	done := func(result string) {
		once.Do(func() { resultCh <- result })
	}

	dispatch.MainQueue().DispatchAsync(func() {
		w := appkit.NewWindowWithSize(420, 640)
		objc.Retain(&w)
		w.SetTitle("WinDTT — подтверждение (капча VK)")
		w.Center()

		closeWindow := func() {
			dispatch.MainQueue().DispatchAsync(func() { w.Close() })
		}

		// Срабатывает и на системный крестик, и на наш w.Close() ниже —
		// once.Do() гарантирует, что done() после успеха/ошибки не
		// перезапишется на "cancelled".
		delegate := &appkit.WindowDelegate{}
		delegate.SetWindowWillClose(func(notification foundation.Notification) {
			done("error:cancelled")
		})
		w.SetDelegate(delegate)

		configuration := webkit.NewWebViewConfiguration()
		script := webkit.NewUserScriptWithSourceInjectionTimeForMainFrameOnly(
			captchaBridgeShimJS+captchaInterceptorJS,
			webkit.UserScriptInjectionTimeAtDocumentStart,
			true, // forMainFrameOnly
		)
		configuration.UserContentController().AddUserScript(script)

		view := webkit.NewWebViewWithFrameConfiguration(foundation.Rect{}, configuration)

		// Хендлеры выполняются в своей горутине (см. darwinkit
		// webkit_custom.go: UserContentControllerDidReceiveScriptMessage
		// оборачивает вызов в go func()), поэтому closeWindow() сам уходит
		// обратно на главный поток через DispatchAsync — вызывать w.Close()
		// не с главного потока нельзя.
		webkit.AddScriptMessageHandler(view, "wdttCaptchaSuccess", func(message objc.Object) {
			// message.Description() для NSString возвращает саму строку
			// (-description у NSString == self) — тот же приём использует
			// официальный пример darwinkit (_examples/webview) для чтения
			// тела сообщения в AddScriptMessageHandlerWithReply.
			done(message.Description())
			closeWindow()
		})
		webkit.AddScriptMessageHandler(view, "wdttCaptchaError", func(message objc.Object) {
			done("error:" + message.Description())
			closeWindow()
		})

		w.SetContentView(view)
		w.MakeKeyAndOrderFront(nil)

		webkit.LoadURL(view, redirectURI)
	})

	onResult(<-resultCh) // блокируем горутину-вызывающего, как и на Windows
}
