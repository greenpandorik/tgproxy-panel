# Движок ноды telemt — дизайн (фаза 4)

Дата: 2026-09-06. Решения заказчика: telemt становится основным движком новых нод; старый стек (`tproxy-server` + официальный MTProxy) остаётся как выбираемый вариант; каждый ключ получает две ссылки: WEB и Fake-TLS. Исследование: `docs/research/2026-09-06-telemt-and-meko.md`.

## 1. Что меняется по сути

Нода с движком `telemt` это один процесс telemt (закреплённая версия, сейчас `3.5.7`), который держит:
- WEB-листенер на loopback (`127.0.0.1:18080`, `transport = "web"`), за Caddy на 443 (TLS, сайт-заглушка как decoy, `X-Forwarded-For`);
- Fake-TLS листенер на `classic_port` (по умолчанию 8443) с SNI `tls_domain` = домен самой ноды (неизвестный SNI и клиенты без секрета уходят на настоящий сайт ноды через `mask`), встроенный `synlimit = "nftables"` (фикс MEKO);
- Control API на `127.0.0.1:9091` с `auth_header`, Prometheus `/metrics` на `127.0.0.1:9090`.

Ключ панели = пользователь telemt (`[access.users]`), профиль на ноде = пользователь + WEB-профиль `[[web.vhosts.profiles]]` с `secret_mode = "plain"`. Применение изменений = вызовы API (`/v1/users`, `PATCH /v1/config`, `POST /v1/system/reload`) без рестарта и без разрыва сессий. Рестарт нужен только при смене листенеров или `web.limits`.

Старый движок (`tproxy`) не меняется; выбор движка фиксируется при создании ноды.

## 2. Модель данных

- `nodes.engine` enum `tproxy | telemt` (default `telemt` для новых, миграция выставляет `tproxy` существующим).
- `nodes.tls_domain` (для telemt; default = hostname), `nodes.classic_port` (int, default 8443), `nodes.telemt_version` (строка, из `GET /v1/system/info`), `nodes.public_ip` становится обязательным для telemt (нужен для `web.vhosts.public_addr`; установочный скрипт определяет его сам и передаёт при регистрации).
- `access_keys` получает `telemt_limits jsonb`: `{data_quota_bytes, rate_limit_up_bps, rate_limit_down_bps, max_unique_ips, max_tcp_conns}`; `expires_at` пробрасывается в `expiration_rfc3339`.
- `key_stats_snapshots(id, access_key_id, node_id, taken_at, connections int, total_octets bigint, quota_used_bytes bigint, active_ips int)` — снимки по ключу с telemt-нод, 30 дней.
- Профили telemt-нод хранят `name` = username в telemt (тот же `k<12hex>`), `secret_enc`, `carrier_mode` (в WEB-профиле telemt carrier глобальный, поле остаётся для tproxy).

## 3. Агент

Новый пакет `internal/telemt` (клиент API): `Client{BaseURL, AuthHeader}`; методы `Health`, `SystemInfo`, `ListUsers`, `CreateUser`, `PatchUser`, `DeleteUser`, `RotateSecret`, `Enable/Disable`, `GetConfig`, `PatchConfig(patch map)`, `Reload`, `ConnectionsSummary`, `Metrics`. Ошибки telemt (`{ok:false, error:{code,message}}`) поднимаются как `*telemt.APIError`.

`agent init-node --engine telemt --hostname H --public-ip IP --tls-domain D --classic-port P --web-user NAME --web-secret HEX`: генерирует `/etc/telemt/telemt.toml` из шаблона (см. §5), API-токен (32 байта, в `/etc/telemt/api.token` 0600 и в `agent.env` как `TGWP_TELEMT_API_TOKEN`), systemd-юнит telemt (из их `install.sh`), декой `/var/lib/telemt/public` (наш сайт-бандл).

`Apply` для telemt: (1) список пользователей из API; (2) для каждого желаемого профиля `create` или `patch` (secret, лимиты, expiration, enabled); (3) удалить лишних пользователей, кроме служебных; (4) `PATCH /v1/config {"web":{"vhosts":[{... "profiles":[...]}]}}` заменой массива профилей; (5) если сайт изменился, записать директорию и `POST /v1/system/reload`; (6) `GET /v1/health/ready`. Никаких рестартов; `ApplyResult.RestartedRelay=false`. Откат: при ошибке на шаге N повторить прежний список пользователей/профилей (панель присылает полное желаемое состояние, а прежнее агент снимает в начале).

Health: `GET /v1/health` + `/v1/health/ready` + `systemctl is-active telemt caddy`. Stats: `GET /v1/users` (квоты/статус), `GET /v1/runtime/connections/summary` (`runtime_edge_enabled = true`) для соединений и октетов по пользователю, `GET /metrics`. Logs: `journalctl -u telemt`.

## 4. Панель

- Создание ноды: выбор движка (telemt по умолчанию), для telemt поля `tls_domain` (предзаполнен hostname) и `classic_port`.
- Установочный скрипт: ветка для telemt (Caddy + telemt из релиза с проверкой sha256 + агент `init-node --engine telemt`), общая с tproxy-веткой регистрация.
- Ссылки ключа на telemt-ноде: `web` (`https://t.me/webproxy?server=H&secret=S`, `tg://webproxy?...`) и `tls` (`https://t.me/proxy?server=H&port=P&secret=ee<S><hex(D)>`, `tg://proxy?...`). API `GET /keys/{id}/links` возвращает `{node_id, node_name, hostname, links:[{kind:"web"|"tls", tme, tg}]}`; QR по `?node=&kind=`. Для tproxy-нод только `web`.
- Лимиты ключа в UI (создание, дровер): квота трафика (ГБ), скорость вверх/вниз (Мбит/с), макс. уникальных IP, макс. соединений. Для tproxy-нод эти поля показываются как «недоступно на этом движке».
- Статистика по ключу: в дровере ключа блок «Трафик и соединения» по нодам (из `key_stats_snapshots`), в таблице ключей колонка «Трафик» (сумма за 30 дней).
- Страница подписки: обе ссылки на локацию.
- Мониторинг: для telemt-нод метрики `telemt_connections_total` и др. вместо `tproxy_*`; парсер расширяется, поля снимка те же (sessions_live ← соединения, bytes ← октеты).

## 5. Шаблон telemt.toml (генерируется агентом)

```toml
[general]
use_middle_proxy = false
log_level = "normal"
[general.modes]
classic = false
secure = false
tls = true
[general.links]
show = []
public_host = "{{hostname}}"
public_port = {{classic_port}}
[server]
port = {{classic_port}}
metrics_listen = "127.0.0.1:9090"
metrics_whitelist = ["127.0.0.1/32"]
[server.api]
enabled = true
listen = "127.0.0.1:9091"
whitelist = ["127.0.0.1/32"]
auth_header = "{{api_token}}"
runtime_edge_enabled = true
[[server.listeners]]
ip = "0.0.0.0"
port = {{classic_port}}
synlimit = "nftables"
[[server.listeners]]
ip = "127.0.0.1"
port = 18080
transport = "web"
proxy_protocol = false
web_client_ip_source = "x_forwarded_for"
web_trusted_proxy_cidrs = ["127.0.0.1/32"]
[censorship]
tls_domain = "{{tls_domain}}"
mask = true
unknown_sni_action = "mask"
[access.users]
{{web_user}} = "{{web_secret}}"
[web]
enabled = true
carrier = "https"
carriers = ["websocket-lanes", "websocket", "https-lanes"]
[[web.vhosts]]
host = "{{hostname}}"
public_addr = "{{public_ip}}:443"
[web.vhosts.decoy]
mode = "static_directory"
directory = "/var/lib/telemt/public"
index = "index.html"
[[web.vhosts.profiles]]
user = "{{web_user}}"
secret_mode = "plain"
```

Caddy на ноде: `{{hostname}} { reverse_proxy 127.0.0.1:18080 { header_up X-Forwarded-For {remote_host} } }` с `trusted_proxies`. Сайт-заглушка отдаётся telemt (decoy), не Caddy, чтобы поведение для валидных и невалидных запросов совпадало.

## 6. Тестовый стенд

`fakenode-telemt`: контейнер с telemt из релиза (x86_64-musl), агентом в режиме telemt, без Caddy; `make e2e-telemt` создаёт ноду с движком telemt, ключ, проверяет `GET /v1/users` на ноде, ссылки обоих видов и статус `active` без рестарта. Реальное подключение из Telegram по-прежнему требует VPS.

## 7. Риски и правила

- Версия telemt закреплена (`TELEMT_VERSION`), sha256 из релиза проверяется; апгрейд версии = отдельная команда установки.
- Панель общается с telemt только через `internal/telemt`; все поля API, которые мы не используем, игнорируются.
- Если telemt меняет контракт, тесты адаптера на фейковом сервере (`httptest`) ломаются первыми.
