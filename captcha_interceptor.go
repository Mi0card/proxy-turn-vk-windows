package main

// captchaInterceptorJS перехватывает решение ручной VK-капчи внутри нативного
// вебвью. Страница id.vk.ru/not_robot_captcha никак не показывает токен
// пользователю — он приходит только в ответе фонового запроса
// captchaNotRobot.check, поэтому единственный способ его получить — патчить
// fetch/XHR и читать JSON-ответ. Портировано 1:1 из ManlCaptchaActivity.kt
// апстрим Android-приложения (amurcanov/proxy-turn-vk-android) — там тот же
// приём через window.WdttCaptcha.onSuccess/onError.
//
// Общий для всех платформ: скрипт вызывает глобальные функции
// window.wdttCaptchaSuccess(token) / window.wdttCaptchaError(msg). Каждая
// платформенная реализация обязана обеспечить их существование ДО того, как
// этот скрипт может сработать:
//   - Windows (captcha_webview_windows.go): их создаёt webview2.Bind() —
//     сама биндинг-инфраструктура WebView2 регистрирует эти имена как вызываемые
//     нативные функции.
//   - macOS (captcha_webview_darwin.go): в WKWebView нет автоматических
//     нативных глобалов — только window.webkit.messageHandlers.<name>.postMessage(),
//     поэтому туда сначала внедряется отдельный тонкий JS-шим
//     (captchaBridgeShimJS), который оборачивает postMessage в такие же
//     window.wdttCaptchaSuccess/Error, и уже потом — этот скрипт.
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
