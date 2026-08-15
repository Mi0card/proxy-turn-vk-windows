# WinDTT

Windows и macOS GUI-клиент для туннеля [WireGuard over VK TURN](https://github.com/amurcanov/proxy-turn-vk-android).

Движок (`go_client`, `server_src`) — из репо [amurcanov/proxy-turn-vk-android](https://github.com/amurcanov/proxy-turn-vk-android), синхронизируется автоматически.
GUI — Go + [Wails v2](https://wails.io) + Vanilla JS.

> Образовательный проект. Не предназначен для производственного использования.

## Скачать

[Releases](../../releases) или [Actions](../../actions) → последний успешный билд → артефакт.

## Возможности

- Подключение по `wdtt://` ссылке или вручную
- SOCKS5 + HTTP прокси с авторизацией через WireGuard userspace (gvisor netstack), без прав администратора
- **Системный прокси** — чекбокс в шапке, перенаправляет WinINET-трафик (Chrome, Edge, Office) через туннель. Отдельный HTTP-прокси на случайном порту, без auth, без прав администратора
- **Маршрутизация по правилам** — вкладка «Маршрутизация»: правила вида `ruleset:geosite-<группа>` / `ruleset:geoip-<группа>` с политиками block / direct / proxy
- SSH деплой `wdtt-server` на VPS с проверкой fingerprint
- Авто-загрузка WireGuard конфига, статус-бар с пингом через туннель
- Светлая / тёмная тема
- Ручная капча VK решается в нативном окне (Windows — WebView2, macOS — WKWebView через darwinkit; на остальных ОС — заглушка). Пользователь проходит проверку, токен уходит в туннель сам

## Системный прокси

Чекбокс **Системный прокси** в шапке приложения.

При включении поднимается отдельный HTTP-прокси на случайном порту (`127.0.0.1:<port>`, без аутентификации) и прописывается в настройках Windows (WinINET, ветка `HKCU`). Трафик идёт **только** через туннель — без туннеля браузеры получат ошибку прокси.

- Требуется активный туннель. Без него запросы браузеров будут отклоняться (502).
- Прав администратора не требуется.
- При закрытии или аварийном завершении приложения настройки восстанавливаются автоматически.

## Маршрутизация по правилам

Вкладка **Маршрутизация** позволяет направлять трафик по правилам (аналогично приложению Throne). Правило задаётся в формате `ruleset:<тип>-<группа>` и может ссылаться на любую группу из скачанных дата-файлов:

- `ruleset:geosite-category-ru` — домены группы `CATEGORY-RU` из geosite.dat
- `ruleset:geosite-youtube` — домены группы `YOUTUBE` из geosite.dat
- `ruleset:geoip-private` — подсети группы `PRIVATE` из geoip.dat

Список групп не ограничен примерами — можно использовать любую группу из geosite.dat / geoip.dat.

Каждому правилу задаётся политика:

- **block** — заблокировать соединение (403 / отклонение SOCKS5)
- **direct** — напрямую, в обход туннеля
- **proxy** — через туннель (по умолчанию)

Правила применяются сверху вниз — выигрывает первое совпадение.

Дата-файлы скачиваются и кешируются из [runetfreedom/russia-v2ray-rules-dat](https://github.com/runetfreedom/russia-v2ray-rules-dat) (кнопка **Обновить правила** на вкладке). Парсер работает с бинарным protobuf-форматом (v2fly) без дополнительных зависимостей.

## Сборка

Требуется: Go 1.21+, [Wails CLI](https://wails.io/docs/gettingstarted/installation), GCC (MSYS2).

```powershell
# Windows (PowerShell)
.\build.ps1
# Результат: build\bin\WinDTT-v{version}.exe
```

Или вручную:

```bash
cd go_client && go mod tidy && go build -ldflags="-s -w" -o ../wdtt-client.exe . && cd ..
cd server_src && GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o ../assets/server/wdtt-server . && cd ..
go mod tidy && wails build -ldflags "-s -w"
```

CI: автоматически при изменении `AppVersion` в `app.go`.

## macOS

Готовая сборка (`.app`) — на странице **Releases**.

**Gatekeeper при первом запуске.** Скачанный `.app` помечается атрибутом карантина — macOS покажет "приложение повреждено" или заблокирует запуск. Перед первым запуском одно из двух:
```bash
xattr -cr WinDTT.app
```
либо в Finder: правый клик по `WinDTT.app` → **Открыть** (не двойной клик) → подтвердить в диалоге.

Сборка подписана ad-hoc (без Apple Developer ID) — полноценной нотаризации нет, предупреждение Gatekeeper это не убирает, только описанный выше шаг.

**Системный прокси недоступен.** На Windows чекбокс "Системный прокси" автоматически редиректит трафик браузера/Office через WinINET/реестр — на macOS такого API в приложении нет, чекбокс скрыт. Доступны SOCKS5 (`127.0.0.1:1080`) и HTTP (`127.0.0.1:1081`) прокси — их нужно прописать вручную: System Settings → Network → выбрать сеть → Details → Proxies, либо через терминал:
```bash
networksetup -setsocksfirewallproxy Wi-Fi 127.0.0.1 1080
```
(порты — see UI приложения, могут отличаться от дефолтных).

## Структура

```
proxy-turn-vk-windows/
├── app.go               ← backend: туннель, деплой, конфиг
├── proxy.go             ← SOCKS5 + HTTP прокси, WireGuard netstack
├── ruleset_manager.go   ← маршрутизация по правилам (geosite/geoip)
├── system_proxy.go      ← системный прокси (WinINET)
├── syscall_windows.go   ← WinINET: реестр + InternetSetOption
├── syscall_unix.go      ← заглушки для кросс-компиляции
├── main.go              ← точка входа, go:embed
├── build.ps1            ← скрипт сборки (Windows)
├── go_client/           ← движок (автосинк)
├── server_src/          ← серверная часть (автосинк)
└── frontend/            ← HTML + CSS + JS
```

## Зависимости

| Пакет | Назначение |
|-------|-----------|
| `github.com/wailsapp/wails/v2` | GUI framework |
| `golang.org/x/crypto/ssh` | SSH деплой |
| `golang.org/x/sys/windows/registry` | Системный прокси |
| `golang.zx2c4.com/wireguard` | WireGuard userspace |
| `gvisor.dev/gvisor` | Userspace TCP/IP стек |
| `github.com/jchv/go-webview2` | Нативное окно WebView2 для решения ручной капчи VK (Windows) |

## Лицензия

GPL-3.0
