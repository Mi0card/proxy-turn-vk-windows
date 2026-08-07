//go:build !windows

package main

// openCaptchaWebView на macOS/Linux пока не реализован — нет аналога WebView2
// с перехватом сети из этого репозитория. Возвращаем ошибку сразу, чтобы
// go_client не завис на таймауте капчи (см. captcha_webview_windows.go).
func openCaptchaWebView(redirectURI string, baseDir string, onResult func(result string)) {
	onResult("error:webview captcha не поддерживается на этой ОС")
}
