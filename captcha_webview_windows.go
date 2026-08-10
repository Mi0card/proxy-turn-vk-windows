//go:build windows

package main

import (
	"path/filepath"
	"runtime"
	"sync"

	webview2 "github.com/jchv/go-webview2"
)

// captchaInterceptorJS теперь общий для всех платформ — см. captcha_interceptor.go.

// openCaptchaWebView открывает нативное окно WebView2 с капчей VK и решает её
// автоматически через перехват сети (см. captchaInterceptorJS) — пользователь
// просто визуально проходит чекбокс/слайдер, токен уходит в go_client сам.
//
// Блокирует вызывающую горутину до результата или закрытия окна пользователем —
// вызывать только через `go openCaptchaWebView(...)`. onResult вызывается ровно
// один раз: токен при успехе, "error:<причина>" при ошибке/отмене — в формате,
// который уже понимает go_client (vk_auth.go: requestWebViewCaptcha).
func openCaptchaWebView(redirectURI string, baseDir string, onResult func(result string)) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var once sync.Once
	done := func(result string) {
		once.Do(func() { onResult(result) })
	}

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		DataPath:  filepath.Join(baseDir, "webview2_captcha"),
		WindowOptions: webview2.WindowOptions{
			Title:  "WinDTT — подтверждение (капча VK)",
			Width:  420,
			Height: 640,
			Center: true,
		},
	})
	if w == nil {
		done("error:webview2 недоступен")
		return
	}
	// Закрываем окно тем же путём, что и нажатие системного крестика (WM_CLOSE),
	// чтобы Run() успел корректно дочитать очередь сообщений и выйти сам —
	// прямой Terminate() отсюда оставил бы окно висеть на экране без обработчика.
	closeWindow := func() { w.Destroy() }

	w.Bind("wdttCaptchaSuccess", func(token string) {
		done(token)
		closeWindow()
	})
	w.Bind("wdttCaptchaError", func(errMsg string) {
		done("error:" + errMsg)
		closeWindow()
	})

	w.Init(captchaInterceptorJS)
	w.Navigate(redirectURI)
	w.Run() // блокирует, пока не Destroy() (см. выше) или пользователь не закроет окно сам

	done("error:cancelled") // Run() вышел без onResult — окно закрыто пользователем
}
