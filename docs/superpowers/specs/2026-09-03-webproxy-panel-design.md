# Telegram WEB Proxy Control Panel — дизайн

Дата: 2026-09-03. Статус: утверждён заказчиком в чате.

Исходное ТЗ: файл `webproxy-panel-build-prompt.md`, переданный заказчиком. Этот документ фиксирует принятые решения
и расхождения ТЗ с реальным `telegramdesktop/tproxy-server` (коммит `52a5feb`, 2026-08-24).

## 1. Цель

Самостоятельная панель управления (control plane) на Go, которая управляет множеством нод с
официальным стеком Telegram WEB Proxy (`tproxy-server` + официальный MTProxy + Caddy), выдаёт
ключи подключения (shared и personal) в виде ссылок `https://t.me/webproxy?server=…&secret=…`
и QR-кодов, следит за состоянием нод, задаёт уникальные сайты-заглушки и брендируется.

Объём: все три итерации ТЗ (MVP, итерация 2, итерация 3), реализуются последовательно фазами.

## 2. Факты о tproxy-server, на которые опирается дизайн

Сверено с исходниками, не с ТЗ. Там, где ТЗ расходится, побеждает репозиторий.

| Факт | Следствие для панели |
|---|---|
| Admin-эндпоинты relay `/healthz`, `/readyz`, `/metrics` слушают `127.0.0.1:8081`. Эндпоинта `/stats` у relay нет. | Агент читает health и метрики с 8081. |
| `/stats` есть у официального MTProxy на `127.0.0.1:8888`. | Агент читает статистику MTProxy с 8888. |
| Relay обрабатывает только SIGINT/SIGTERM. Горячей перезагрузки профилей нет. | Применение профилей = `systemctl restart tproxy-server`. Кнопки «reload» нет. |
| Профили грузятся через systemd `LoadCredential` из `/etc/tproxy-server/profiles.json` (0400, root:tproxy). | Агент пишет файл атомарно и рестартит сервис. |
| `limits.max_profiles` в config.json, по умолчанию 32, проверяется при старте и при `-check`. | Инсталлер панели ставит 128. UI показывает занятость. |
| Секрет профиля = client-facing MTProxy secret. MTProxy принимает секреты только через флаг `-S` (несколько флагов допустимы при общей политике). | Каждый новый personal-ключ требует перезаписи `mtproxy.env` и рестарта mtproxy. |
| Секрет: 16 байт hex (32 символа), опционально префикс `dd` (17 байт). Relay также принимает base64url. | Панель генерирует 32 hex без `dd`. |
| Relay при старте загружает `public_dir` в память. Юнит relay держит `/srv/tproxy-site` read-only. | Смена сайта = запись от root + рестарт relay. |
| Caddy ставит CSP: без inline-стилей/скриптов, без внешних ресурсов, без воркеров и фреймов. | Валидатор заглушек выносит `<style>`/`<script>` в файлы. |
| Метрики relay глобальные, без метки профиля. MTProxy тоже не сообщает, какой секрет использован. | Статистики по ключу нет. Есть только `limits` профиля. |
| Рестарт relay инвалидирует carrier-сессии; клиент пересоздаёт их сам. | Применять изменения батчем, не на каждое действие. |
| `-check` флаг валидирует config + profiles без запуска. | Агент прогоняет `-check` перед записью. |
| Клиенты: Desktop стабильно, Android экспериментально, iOS в планах. | Плашка при выдаче ключа. |

## 3. Архитектура

### 3.1 Компоненты

```
┌──────────────── Control plane (Docker Compose) ─────────────────┐
│ Caddy(TLS) → panel (Go: chi API + gRPC gateway + workers + SPA) │
│              Postgres 16                                        │
└───────────────────────▲─────────────────────────────────────────┘
                        │ один bidi gRPC-стрим, исходящий от агента (HTTPS)
┌───────────────────────┴─────────── Node ────────────────────────┐
│ agent (Go, systemd, root)                                       │
│ Caddy :80/:443 → tproxy-server 127.0.0.1:8080 → MTProxy :2398   │
│ admin 127.0.0.1:8081 (relay), 127.0.0.1:8888 (MTProxy stats)    │
│ /etc/tproxy-server/{config,profiles}.json, /etc/mtproxy/mtproxy.env, /srv/tproxy-site │
└─────────────────────────────────────────────────────────────────┘
```

Один Go-модуль `github.com/mihailvolkov/tgwebproxy-panel` (имя модуля условное, без публикации):

```
cmd/panel                # control plane
cmd/agent                # нод-агент
internal/domain          # сущности и правила, без внешних зависимостей
internal/store           # sqlc-код, goose-миграции, репозитории
internal/api             # chi-хендлеры, middleware (auth, rbac, csrf, ratelimit)
internal/gateway         # gRPC-сервер для агентов, реестр живых нод
internal/nodedriver      # интерфейс NodeDriver + реализация поверх gateway + mock
internal/crypto          # AES-GCM, argon2id, HMAC-сессии, TOTP
internal/worker          # apply-батчи, сбор stats, истечение ключей, алерты, бэкапы
internal/branding        # BrandingProfile, инжекция CSS-переменных
internal/qrlink          # ссылки t.me/webproxy, tg://webproxy, QR
internal/sitekit         # валидатор/нормализатор/уникализатор заглушек, пресеты
internal/agent           # логика агента: файлы, systemd, health, логи
proto/agent/v1           # protobuf-контракт
web/                     # React SPA, собирается в web/dist, embed в panel
deploy/                  # docker-compose, Caddyfile, node-install.sh, grafana, systemd
docs/
```

### 3.2 Транспорт агент ↔ панель

Агент инициирует соединение: gRPC поверх HTTPS к панели (через Caddy с h2), метод
`AgentGateway.Session(stream Envelope) returns (stream Envelope)`. Первое сообщение от агента:
`Hello{node_token, agent_version, tproxy_version, hostname}`. Далее панель отправляет
`Request{request_id, oneof body}`, агент отвечает `Response{request_id, oneof body}` или серией
`Chunk{request_id, …}` для логов. Агент шлёт `Heartbeat` каждые 30 с. Реконнект с экспоненциальной
задержкой. На ноде не открывается ни одного порта.

Панель хранит реестр живых стримов (`gateway.Registry`). `NodeDriver` — интерфейс:

```go
type NodeDriver interface {
    Health(ctx, nodeID) (HealthReport, error)
    GetProfiles(ctx, nodeID) (ProfilesFile, error)
    ApplyProfiles(ctx, nodeID, ApplyRequest) (ApplyResult, error)   // profiles + mtproxy secrets, атомарно
    GetSite(ctx, nodeID) (SiteBundle, error)
    SetSite(ctx, nodeID, SiteBundle) (ApplyResult, error)
    Metrics(ctx, nodeID) (string, error)       // relay /metrics, Prometheus text
    Stats(ctx, nodeID) (MTProxyStats, error)   // MTProxy /stats
    TailLogs(ctx, nodeID, services []string, lines int) (<-chan LogLine, error)
    RestartRelay(ctx, nodeID) error
}
```

Реализации: `gateway` (боевая) и `mock` (для тестов и локального UI без ноды).

Аутентификация агента: токен ноды (32 байта, base64url) выдаётся при регистрации, в БД хранится
только argon2id-хеш. Токен передаётся в metadata `authorization: Bearer`. Отзыв токена рвёт стрим.

### 3.3 Установка ноды

Панель создаёт ноду в статусе `pending` и одноразовый install-токен (TTL 24 ч). Показывает команду:

```
curl -fsSL https://<panel>/api/v1/install/<token>.sh | sudo bash
```

Скрипт (`deploy/node-install.sh`, шаблонизируется панелью): проверяет ОС/arch, клонирует
`tproxy-server` на закреплённом коммите, запускает официальный `deploy/install.sh` с
`--hostname`, `--email`, `--secret` (первый профиль) и `--site-dir` (заглушка, отрендеренная
панелью и вложенная в скрипт как tar.gz base64), затем ставит бинарь агента (скачивается с панели по
`/api/v1/install/agent/linux-amd64` с проверкой sha256), systemd-юнит агента, переписывает
`config.json` с `max_profiles: 128`, добавляет drop-in `mtproxy.service.d/secrets.conf` с
`ExecStart=` через `$MTPROXY_SECRETS` (несколько `-S`), регистрирует ноду
(`POST /api/v1/install/<token>/register`, получает node_token), запускает агента. Нода переходит в
`online` при первом `Hello`.

### 3.4 Применение изменений (apply pipeline)

Все мутации профилей и сайта не идут на ноду сразу. Они помечают ноду `dirty`. Воркер
`worker/apply` раз в `apply_interval` (настройка, по умолчанию 45 с) или по кнопке «Применить сейчас»
собирает по ноде полное желаемое состояние: список профилей (имя, секрет, backend, carrier_mode,
limits), список секретов MTProxy, актуальный бандл сайта (если изменился). Создаёт `apply_jobs`
запись и вызывает `ApplyProfiles`/`SetSite`.

Агент:
1. пишет кандидаты во временные файлы, прогоняет `tproxy-server -config … -profiles-file <tmp> -check`;
2. бэкапит текущие `profiles.json`, `mtproxy.env`, `/srv/tproxy-site` в `/var/lib/tgwp-agent/backup/<ts>/`;
3. атомарно заменяет файлы (`rename`), права как у install.sh;
4. `systemctl restart mtproxy` (если секреты изменились), `systemctl restart tproxy-server`;
5. ждёт `/healthz` и `/readyz` до 20 с;
6. при ошибке восстанавливает бэкап, рестартит, возвращает ошибку с логом.

Результат пишется в `apply_jobs`, профили получают `sync_state = synced|failed`, ключи
`pending → active`. Идемпотентно: повторный apply с тем же состоянием ничего не рестартит (сравнение
хеша содержимого файлов).

### 3.5 Модель ключей

- `access_keys.type = SHARED`: один секрет, один профиль на каждой привязанной ноде. Ревокация =
  генерация нового секрета профиля (UI предупреждает, что все держатели отвалятся) или удаление.
- `access_keys.type = PERSONAL`: уникальный секрет, отдельный профиль на каждой привязанной ноде.
  Ревокация удаляет профили этого ключа. `expires_at` обрабатывает воркер `worker/expiry`.
- Имя профиля на ноде: `k<short-id>` (до 64 символов, уникально в ноде).
- Секрет генерируется панелью: 16 случайных байт → 32 hex.
- Лимиты ключа (`limits` jsonb) пробрасываются в профиль как есть, валидируются по правилам relay
  (не больше глобальных).
- Ёмкость ноды: `nodes.max_profiles` (128 после установки). При превышении создание ключа на эту
  ноду отклоняется с понятной ошибкой.
- Batch-создание personal: N ключей по шаблону имени `{prefix}-{n}`.

Ссылки: `https://t.me/webproxy?server=<hostname>&secret=<hex>` и `tg://webproxy?server=…&secret=…`.
Для ключа на нескольких нодах: список ссылок по одной на hostname + страница подписки (§3.8).

### 3.6 Сайты-заглушки

`site_templates`: пять пресетов (разная вёрстка: студия, блог, лендинг продукта, портфолио,
документация) + пользовательские. Хранится один HTML + `assets` (map path → bytes, base64 в jsonb).

`sitekit.Normalize(html)`: парсит HTML (golang.org/x/net/html), удаляет/отклоняет:
- любые URL с внешними хостами в `src`, `href` (кроме `mailto:`), `@import`, `url()`;
- атрибуты `on*`, `javascript:` в href;
- `<link rel=manifest>`, service worker регистрации, `<iframe>`, `<base>`, `<form>`;
- выносит каждый `<style>` в `/s-<rand>.css`, каждый inline `<script>` в `/j-<rand>.js`,
  атрибуты `style=""` в класс `.i-<rand>` в общем CSS.
Возвращает бандл `{index.html, files}` и список ошибок (что запрещено) / предупреждений (что изменено).

`sitekit.Uniquify(bundle, seed)`: детерминированно по seed (node_id) переставляет секции с
атрибутом `data-block`, переименовывает классы, выбирает варианты текстов из `data-variants`,
меняет имена файлов ассетов. Пресеты размечены под это.

Назначение шаблона ноде создаёт `node_sites` (rendered bundle) и помечает ноду `dirty`.

### 3.7 Мониторинг и алерты

Агент в каждом `Heartbeat` отдаёт: статусы юнитов `tproxy-server`, `mtproxy`, `caddy`
(`systemctl is-active`), `/healthz`, `/readyz`, версию relay (git-коммит из установки), uptime,
загрузку CPU/RAM/диска. Воркер `worker/stats` раз в 60 с запрашивает `Metrics` и `Stats`, парсит
Prometheus-текст (`tproxy_sessions_live`, `tproxy_streams_live`, `tproxy_bytes_up_total`,
`tproxy_bytes_down_total`, `tproxy_sessions_created_total`, `tproxy_limit_hits_total` и др.) и пишет
`node_stats_snapshots` (raw jsonb + разобранные колонки). Ретеншн 30 дней.

Панель отдаёт `/metrics` (свои метрики + агрегаты по нодам с меткой `node`) и
`/api/v1/nodes/{id}/metrics` (проксирование raw relay-метрик, под токеном Prometheus). В `deploy/grafana`
JSON-дашборд.

Алерты: нода без heartbeat дольше `offline_after` (по умолчанию 90 с) → статус `offline`,
запись в `alerts`, баннер в UI. Опционально: Telegram-бот (bot token + chat_id в settings) получает
сообщение при offline/online и при провале apply.

### 3.8 Страница подписки

`subscription_tokens`: токен (32 байта base64url) ↔ access_key. Публичный роут
`GET /s/<token>` отдаёт серверно отрендеренную страницу (Go html/template, без SPA): название бренда,
плашка о поддержке клиентов, для каждой привязанной ноды: название, ссылка, QR (data URI), кнопка
копирования. `GET /s/<token>.json` отдаёт то же в JSON. Ревокация ключа делает страницу 410.

### 3.9 Брендинг и white-label

`branding_profiles`: несколько наборов, флаг `is_active` ровно у одного. Поля: panel_name, logo,
favicon (файлы в `data/branding/<id>/`), primary_color, accent_color, theme_default, login_bg,
login_text, support_link, footer_text, custom_css. `GET /api/v1/branding` публичный (нужен до логина),
SPA инжектит переменные в `:root`. `custom_css` санитизируется: запрет `@import`, `url(` с внешними
хостами, `expression(`. Логотип/favicon: png/svg/ico до 512 КБ, svg проходит через санитайзер (без
`<script>`, `on*`, внешних href).

### 3.10 Безопасность

- Пароли argon2id (m=64MiB, t=3, p=2). Первый owner создаётся CLI: `panel admin create`.
- Сессии: случайный id в БД + HMAC-SHA256 подпись в cookie `httponly; secure; samesite=lax`,
  TTL 24 ч, продлевается. Смена пароля удаляет все сессии пользователя.
- CSRF: double-submit cookie + заголовок `X-CSRF-Token` на всех не-GET.
- Rate limit на `/auth/login`: 10 попыток за 10 мин с IP, затем блок IP на 15 мин; отдельно
  блок аккаунта после 20 неудач.
- 2FA TOTP (RFC 6238) за флагом `FEATURE_TOTP=true`: включение через QR, 8 recovery-кодов.
- RBAC: `owner` (всё, включая админов и ротацию ключей), `admin` (всё кроме управления
  админами/настроек безопасности), `viewer` (только чтение, без секретов).
- Секреты в БД: AES-256-GCM, мастер-ключ `MASTER_KEY` (32 байта base64) из env. В каждом
  зашифрованном поле префикс версии ключа. Ротация: `panel keys rotate --new-key …` перешифровывает
  все поля в транзакции.
- Аудит: middleware пишет все мутации (кто, что, цель, meta без секретов, IP).
- Логи: slog JSON, секреты/токены/bridge-URL никогда не логируются (redact в типах).
- Бэкапы: `pg_dump` по кнопке и по cron-расписанию из settings в `data/backups/`, ретеншн N штук,
  восстановление CLI `panel db restore <file>`.

### 3.11 Фронтенд

React 18 + TypeScript strict + Vite + Tailwind + shadcn/ui, `lucide-react`, `recharts`,
`react-hook-form` + `zod`, `@tanstack/react-query`, `@tanstack/react-table`, `i18next` (ru по умолчанию,
en; определение сохранённого выбора в localStorage). Тёмная тема по умолчанию, переключатель.

Разделы: Login, Dashboard (карточки-метрики, графики, алерты), Nodes (таблица, карточка ноды:
health, профили, логи, stats, сайт, кнопки restart/apply), Keys (таблица с фильтрами, bulk-actions,
модалка создания shared/personal/batch, модалка ссылки+QR с плашкой о клиентах), Site Templates
(библиотека, редактор с live-валидацией, назначение нодам), Monitoring (графики по нодам, Prometheus
инструкции), Audit (таблица), Settings (брендинг с live-preview, безопасность, 2FA, админы,
бэкапы, алерты, apply-интервал).

Адаптив: 360/768/1280; сайдбар → drawer; таблицы → карточки. Все тексты в словарях. Без
маркетингового тона.

### 3.12 Локальная разработка и тесты

- `deploy/docker-compose.yml`: `panel`, `postgres`, `caddy`, `fakenode` (образ с собранным
  tproxy-server на закреплённом коммите, агентом, systemd-эмуляцией через простой supervisor-скрипт и
  TCP-заглушкой на 2398/8888 вместо MTProxy). `fakenode` регистрируется автоматически по
  `INSTALL_TOKEN` из env.
- Локально без Docker: Postgres из Homebrew, `make dev` поднимает panel с `NODE_DRIVER=mock`.
- Тесты: `go test ./...` (unit: crypto, qrlink, sitekit, domain-правила, apply-планировщик;
  интеграционные: store и API против Postgres по `TEST_DATABASE_URL`, пропускаются без него);
  фронт: `tsc --noEmit`, eslint, vitest на утилиты; e2e-smoke: скрипт создаёт ключ через API и
  проверяет ссылку и QR.
- Линт: golangci-lint, gofumpt, eslint + prettier.

## 4. Схема данных (goose-миграции)

```
admin_users(id uuid pk, username unique, password_hash, role, totp_secret_enc null,
            totp_enabled bool, failed_logins int, locked_until null, created_at)
recovery_codes(id, admin_user_id fk, code_hash, used_at null)
sessions(id text pk, admin_user_id fk, created_at, expires_at, ip, user_agent)
nodes(id uuid pk, name, hostname unique, public_ip, status enum(pending,online,offline,degraded),
      agent_token_hash, install_token_hash null, install_token_expires null,
      tproxy_version, agent_version, max_profiles int default 128, dirty bool,
      last_seen_at, last_apply_at, created_at)
profiles(id uuid pk, node_id fk, access_key_id fk null, name, secret_enc, backend,
         carrier_mode, limits jsonb, sync_state enum(pending,synced,failed), created_at,
         unique(node_id, name))
access_keys(id uuid pk, label, type enum(SHARED,PERSONAL), owner_label, secret_enc,
            status enum(pending,active,revoked), carrier_mode, limits jsonb,
            expires_at null, revoked_at null, note, created_by fk, created_at)
key_bindings(access_key_id fk, node_id fk, profile_id fk, pk(access_key_id,node_id))
site_templates(id uuid pk, name, html text, assets jsonb, is_preset bool, created_at, updated_at)
node_sites(node_id pk fk, template_id fk, bundle jsonb, bundle_hash, deployed_hash null, updated_at)
apply_jobs(id uuid pk, node_id fk, status enum(queued,running,ok,failed,rolled_back),
           kind enum(profiles,site,both), started_at, finished_at, error text, log text)
branding_profiles(id uuid pk, name, is_active bool, panel_name, logo_path, favicon_path,
                  primary_color, accent_color, theme_default, login_bg_path, login_text,
                  support_link, footer_text, custom_css, updated_at)
audit_log(id bigserial, admin_user_id fk null, action, target_type, target_id, meta jsonb, ip, created_at)
node_stats_snapshots(id bigserial, node_id fk, taken_at, sessions_live int, streams_live int,
                     bytes_up bigint, bytes_down bigint, sessions_created bigint, limit_hits bigint,
                     mtproxy_raw jsonb, relay_raw text)
alerts(id bigserial, node_id fk null, kind, message, created_at, resolved_at null)
subscription_tokens(token_hash pk, access_key_id fk, created_at, revoked_at null)
settings(key pk, value jsonb)   -- apply_interval, offline_after, telegram_alerts, backup_schedule
backups(id, path, size, created_at, kind enum(manual,scheduled))
```

## 5. API (chi, префикс `/api/v1`, JSON)

- `POST /auth/login`, `POST /auth/logout`, `GET /auth/me`, `POST /auth/totp/{setup,confirm,disable}`
- `GET /branding` (публично), `GET|PUT /branding/profiles`, `POST /branding/profiles/{id}/activate`,
  `POST /branding/profiles/{id}/logo`
- `GET|POST /nodes`, `GET|PATCH|DELETE /nodes/{id}`, `POST /nodes/{id}/apply`, `POST /nodes/{id}/restart`,
  `GET /nodes/{id}/health`, `GET /nodes/{id}/profiles`, `GET /nodes/{id}/stats`,
  `GET /nodes/{id}/logs` (SSE), `GET /nodes/{id}/metrics`, `GET /nodes/{id}/install-command`,
  `POST /nodes/{id}/check` (DNS/порты/сертификат)
- `GET /install/{token}.sh` (публично), `POST /install/{token}/register`, `GET /install/agent/{platform}`
- `GET|POST /keys`, `POST /keys/batch`, `GET|PATCH|DELETE /keys/{id}`, `POST /keys/{id}/revoke`,
  `POST /keys/{id}/rotate`, `GET /keys/{id}/links`, `GET /keys/{id}/qr?node=…` (png),
  `POST /keys/{id}/subscription` (создать/пересоздать токен), `POST /keys/bulk` (revoke/delete/extend)
- `GET|POST /site-templates`, `GET|PUT|DELETE /site-templates/{id}`, `POST /site-templates/validate`,
  `POST /nodes/{id}/site` (назначить шаблон), `GET /nodes/{id}/site/preview`
- `GET /dashboard/summary`, `GET /monitoring/nodes/{id}/series?from&to&step`, `GET /alerts`,
  `POST /alerts/{id}/resolve`
- `GET /audit`
- `GET|PUT /settings`, `GET|POST /admins`, `PATCH|DELETE /admins/{id}`, `POST /admins/{id}/reset-password`,
  `POST /me/password`, `GET|POST /backups`, `GET /backups/{id}/download`
- Публично: `GET /s/{token}`, `GET /s/{token}.json`, `GET /metrics` (под `METRICS_TOKEN`), `GET /healthz`

Ошибки: `{ "error": { "code": "...", "message": "...", "fields": {...} } }`. Пагинация:
`?page&per_page&sort&order`, ответ `{ items, total }`.

## 6. Фазы

**Фаза 1 (MVP):** каркас, миграции, crypto, auth без 2FA, nodes + install + gateway + agent,
apply-pipeline, keys shared/personal/batch + ссылки + QR, один пресет + валидатор, дашборд с базовыми
метриками, брендинг (один профиль), SPA с разделами Login/Dashboard/Nodes/Keys/Site Templates/Settings,
ru/en, compose с fakenode.

**Фаза 2:** графики мониторинга и Monitoring-раздел, RBAC, Audit-раздел, ещё четыре пресета +
уникализация, алерты (UI + Telegram), мультилокация в UI ключа, проверка предпосылок ноды.

**Фаза 3:** TOTP, страница подписки, несколько профилей брендинга, Grafana-дашборд + Prometheus-док,
бэкапы по кнопке и расписанию, ротация мастер-ключа.

## 7. Определение готовности

Из ТЗ §13 плюс: `fakenode` в compose проходит apply с реальной валидацией `-check`; смена сайта
проходит валидатор и отдаётся relay в fakenode; `go test ./...`, `golangci-lint`, `tsc`, eslint зелёные.
Пункт про реальное подключение из Telegram Desktop проверяется, когда появится тестовый VPS.
