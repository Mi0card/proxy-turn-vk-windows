//go:build windows

package main

import (
	"path/filepath"
	"runtime"
	"sync"

	webview2 "github.com/jchv/go-webview2"
)

// captchaInterceptorJS перехватывает решение ручной VK-капчи внутри WebView2.
// Страница id.vk.ru/not_robot_captcha никак не показывает токен пользователю —
// он приходит только в ответе фонового запроса captchaNotRobot.check, поэтому
// единственный способ его получить — патчить fetch/XHR и читать JSON-ответ.
// Портировано 1:1 из ManlCaptchaActivity.kt апстрим Android-приложения
// (amurcanov/proxy-turn-vk-android) — там тот же приём через
// window.WdttCaptcha.onSuccess/onError; здесь — через webview2.Bind().
const captchaInterceptorJS = `
(function() {
    if (window.__wdtt_interceptor_installed) return;
    window.__wdtt_interceptor_installed = true;

    function handleResponse(data) {
        try {
            if (data.response && data.response.success_token) {
                window.wdttCaptchaSuccess(data.response.success_token);
            } else if (data.error) {
                window.wdttCaptchaError(JSON.stringify(data.error));
            }
        } catch (e) {}
    }

    const origFetch = window.fetch;
    window.fetch = async function() {
        const args = arguments;
        const url = args[0] || '';
        if (typeof url === 'string' && url.includes('captchaNotRobot.check')) {
            const response = await origFetch.apply(this, args);
            const clone = response.clone();
            try { handleResponse(await clone.json()); } catch (e) {}
            return response;
        }
        return origFetch.apply(this, args);
    };

    const origXHROpen = XMLHttpRequest.prototype.open;
    const origXHRSend = XMLHttpRequest.prototype.send;
    XMLHttpRequest.prototype.open = function(method, url) {
        this._wdtt_url = url;
        return origXHROpen.apply(this, arguments);
    };
    XMLHttpRequest.prototype.send = function() {
        const xhr = this;
        if (xhr._wdtt_url && xhr._wdtt_url.includes('captchaNotRobot.check')) {
            xhr.addEventListener('load', function() {
                try { handleResponse(JSON.parse(xhr.responseText)); } catch (e) {}
            });
        }
        return origXHRSend.apply(this, arguments);
    };
})();
`

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
