# TGProxy Panel — ТЗ vNext
## Telemt WEB Proxy, Reliability, Diagnostics, Monitoring и UI/UX

> Актуально для текущего состояния `greenpandorik/tgproxy-panel` на 2026-09-09.  
> Основной современный движок — Telemt 3.5.7. `tproxy` сохраняется как legacy-движок.

> **Ревизия 2026-09-09.** Документ выверен против фактического API и конфига Telemt 3.5.7
> (docs репозитория telemt/telemt на теге 3.5.7 и исходники `src/metrics/web.rs`,
> `src/api/config_store.rs`). Пункты, опиравшиеся на несуществующую телеметрию, исправлены —
> см. §2.1. Реализовывать следует именно эту редакцию.

---

# 1. Цель

Развить TGProxy Panel из панели управления Telegram Proxy в полноценную систему централизованного управления Telegram proxy-инфраструктурой с упором на Telemt WEB Proxy.

Ключевые цели:

- максимально использовать возможности Telemt 3.5.7;
- улучшить устойчивость WEB Proxy на разных клиентах и сетях;
- включить carrier negotiation + carrier learning;
- убрать технические параметры из обычных пользовательских сценариев;
- добавить полноценную диагностику WEB Proxy;
- сделать безопасное обновление Telemt;
- построить мониторинг вокруг реального качества proxy, а не только CPU/RAM/traffic;
- существенно упростить создание ноды и ключа;
- провести полный UI/UX redesign основных экранов;
- разделить Basic и Advanced настройки;
- сохранить текущую архитектуру адаптера Telemt и не размазывать знания о его API по UI/backend.

---

# 2. Что уже есть и должно быть сохранено

Текущая реализация Telemt уже содержит правильную основу:

- отдельный Fake-TLS listener;
- WEB listener на `127.0.0.1:18080`;
- TLS termination через Caddy;
- `X-Forwarded-For`;
- trusted proxy CIDR;
- Control API только на loopback;
- Prometheus metrics только на loopback;
- WEB + Fake-TLS ссылки;
- per-user Telemt limits;
- `synlimit = "nftables"`;
- decoy static site;
- systemd hardening;
- непривилегированный пользователь `telemt`;
- pinned Telemt binary;
- form drafts;
- contextual help;
- RU/EN i18n;
- node CPU/RAM/disk monitoring;
- текущий design system и базовые UI-компоненты.

Не нужно накладывать MEKO SYN-fix поверх Telemt — Telemt уже делает это сам.

Из MEKO имеет смысл переносить только идеи:

- SNI checker;
- network pre-flight;
- environment diagnostics;
- понятный unattended install;
- server optimisation audit;
- firewall/network checks.

---

# 2.1. Ограничения Telemt 3.5.7, обязательные к соблюдению

Проверка реального API показала, что часть исходных требований опиралась на телеметрию,
которой в Telemt 3.5.7 нет. Ниже — обязательные правила.

## Правило отсутствующих данных

UI **никогда** не интерпретирует отсутствующую метрику или capability как `0`.

Если данных нет — показывать `Not available` / «Нет данных» либо не рендерить блок целиком.
Иначе `0 failures`, `0 sessions` или `0 ms` выглядят как настоящие измерения и врут оператору.

Это же правило распространяется на API: отсутствующее значение отдаётся как `null`,
а не как ноль.

## Чего в Telemt 3.5.7 нет

| Исходное требование | Факт | Что делать |
|---|---|---|
| Setup latency P50/P95 | Гистограмм/summary нет ни одной во всём WEB-пути | Убрано из P0 и из WEB Transport. Не подменять временем панели до ноды — это другая величина |
| Fallback rate | Счётчика fallback не существует | Использовать реальные счётчики: carrier failures, rejected attempts, evicted sessions |
| Active sessions by carrier | Gauge живых сессий нет, только монотонные счётчики | Показывать Carrier selection distribution (доли выборов), без формулировки «сейчас активно N» |
| Bridge retries | Есть события восстановления моста, не счётчик повторов | Переименовано в Bridge recoveries |
| capabilities из `/v1/system/info` | Эндпоинт не отдаёт feature-флагов | Panel вычисляет capabilities сам, см. §7 |
| `max_pending_per_session` для Telemt | Ключа не существует | Полностью убрано из Telemt-части, см. §12 |

## Границы значений, нарушение которых Telemt отвергает

- `carrier_negotiation_deadlines_secs` — ровно 4 строго возрастающих значения;
- `carrier_probe_coalesce_ms` — `0..=10`, не произвольное число;
- `carrier_learning_secs` — `2..=86400`; `bridge_request_secs` — `1..=60`;
  `bridge_retry_secs` — `1..=300` и не меньше `bridge_request_secs`;
- `carriers` — непустой массив уникальных значений из
  `https`, `https-lanes`, `websocket`, `websocket-lanes`, либо `false`;
- per-profile лимиты не могут превышать глобальные из `[web.limits]`;
- decoy `http_upstream` принимает только IP-литерал в loopback/private диапазоне
  (`http://127.0.0.1:3000`), хостнеймы отвергаются.

## Hot reload против restart

`PATCH /v1/config` принимает секции `general`, `timeouts`, `censorship`, `upstreams`,
`dc_overrides`, `web` и `server.listeners`; секция `access` отвергается.

Применяются на лету: `carrier`, `carriers`, `carrier_learning`,
`carrier_negotiation_aggressiveness`, `http_connection_capacity_action`, весь `[web.timeouts]`,
vhosts, профили, decoy, `general.ad_tag`.

Сохраняются, но требуют рестарта: весь `[web.limits]`, `censorship.tls_emulation`,
`censorship.tls_front_dir`, `general.use_middle_proxy`, `server.listeners`,
`web.decoy_fasttrack_mode`.

Панель **не имеет права** считать такой PATCH применённым: Telemt возвращает их в
`deferred_process_fields` / `process_restart_required`, и это состояние обязано доходить до UI.

## Протокол WEB-эндпоинтов

Все `POST /v1/runtime/web/*` требуют тело с `runtime_instance` (32 hex), который берётся из
`GET /v1/runtime/web/status`. Несовпадение — `409 web_runtime_mismatch`, и это означает, что
Telemt перезапустился: инстанс нужно перечитать, а не повторять запрос вслепую.

`POST /v1/runtime/web/lifecycle/drain` отвечает `202` и **не ждёт** завершения. Прогресс
читается опросом `GET /v1/runtime/web/status` → `operator_lifecycle.drain`
(`remaining_sessions`, `remaining_streams`, `state`, `outcome`).

---

# 3. P0 — Carrier Learning

## Проблема

Сейчас инфраструктура carrier negotiation уже используется, но сервер не использует полноценное обучение на реально успешных соединениях.

## Требование

Для Telemt включить:

```toml
[web]
enabled = true

carrier = "https"

carriers = [
  "websocket-lanes",
  "websocket",
  "https-lanes"
]

carrier_learning = true
carrier_negotiation_aggressiveness = "conservative"

[web.timeouts]
carrier_negotiation_deadlines_secs = [3, 5, 8, 12]
carrier_health_secs = 30
carrier_learning_secs = 600
bridge_request_secs = 10
bridge_retry_secs = 90
carrier_probe_coalesce_ms = 0
```

## Default policy

```text
Fallback
HTTPS

Negotiation order
1. WebSocket Lanes
2. WebSocket
3. HTTPS Lanes
4. HTTPS

Carrier Learning
Enabled

Aggressiveness
Conservative
```

Важно:

- fallback `https` оставить;
- не пытаться насильно переключать transport у уже живой сессии;
- новая policy применяется к новым сессиям;
- UI не должен заставлять обычного пользователя понимать carrier internals.

---

# 4. P0 — убрать Carrier Mode из Telemt-ключей

## Как сейчас

В `CreateKeyDialog` carrier выбирается как свойство ключа:

- HTTPS;
- HTTPS Lanes;
- WebSocket;
- WebSocket Lanes.

## Проблема

Для Telemt это архитектурно неверное место.

Carrier policy относится к WEB Proxy ноде и negotiation engine, а не к конкретному пользователю.

Из-за этого UI создаёт впечатление, что пользователь вручную закрепляет транспорт за Telemt-ключом.

## Как должно быть

### Если выбраны только Telemt-ноды

Поле `Carrier Mode` вообще не показывать.

Вместо него:

```text
WEB transport
Automatic

The node automatically selects the best transport
for the Telegram client and network.
```

По клику на help:

```text
Negotiation order

WebSocket Lanes
→ WebSocket
→ HTTPS Lanes
→ HTTPS
```

### Если выбраны только tproxy-ноды

Оставить существующий Carrier Mode.

### Если выбраны Telemt + tproxy

Показывать:

```text
Legacy WEB transport
[ HTTPS ▼ ]

Applies only to legacy tproxy nodes.
Telemt nodes use automatic transport selection.
```

---

# 5. P0 — полноценный WEB Proxy Diagnostics

## Проблема

Зелёный статус:

```text
telemt.service active
caddy active
443 open
```

не означает, что Telegram реально способен подключиться.

Нужна диагностика всей цепочки.

## Требование

Добавить endpoint уровня панели:

```text
POST /api/v1/nodes/{id}/diagnostics/web
```

Результат должен быть структурированным.

### DNS

```text
Domain
node1.example.com

DNS A
✓ 95.x.x.x

DNS AAAA
—
```

Проверять также соответствие ожидаемому public IP.

### Public TCP/TLS

```text
TCP :443
✓ 31 ms

TLS handshake
✓

Certificate
Let's Encrypt

Certificate hostname
✓

Expires
47 days

HTTP/2
✓

HTTP/1.1
✓
```

### Reverse Proxy

```text
Caddy
✓ running

WEB upstream
✓ 127.0.0.1:18080

X-Forwarded-For
✓

Trusted proxy
✓

Public addr
✓ 95.x.x.x:443
```

### WEB transport

```text
HTTPS
✓

HTTPS Lanes
✓

WebSocket
✓

WebSocket Lanes
✓
```

### Telemt

```text
Control API
✓

Ready endpoint
✓

WEB runtime
✓

Fake-TLS listener
✓
```

### Telegram connectivity

```text
WEB authentication
✓

Bridge
✓

Telegram DC connectivity
✓
```

## Источники проверок

Собственного self-diagnostics эндпоинта у Telemt нет (`/web-status` — человеческая HTML-страница,
не машинный вердикт). Панель собирает картину сама:

| Блок | Источник |
|---|---|
| DNS, TCP/TLS, сертификат, HTTP/2 | Собственные проверки панели снаружи |
| Reverse proxy, upstream, XFF | Конфиг ноды + проверка агентом |
| WEB transport по carrier'ам | `GET /v1/runtime/web/status`, `GET /v1/runtime/web/sessions` |
| Control API, ready, WEB runtime | `GET /v1/health`, `/v1/health/ready`, `/v1/runtime/web/status` |
| Telegram connectivity | `/v1/health/ready` (`healthy_upstreams`), `/v1/stats/dcs`, `/v1/runtime/me_quality` |

Проверка, для которой источник недоступен, показывается как `Not available` и **не считается**
ни пройденной, ни проваленной — она исключается из счётчика `N / M checks passed`.

## Итоговый статус

Не просто `Healthy`.

Должно быть:

```text
WEB Proxy
Healthy

12 / 12 checks passed
```

или:

```text
WEB Proxy
Degraded

WebSocket Lanes is unavailable.
HTTPS fallback is working.

Impact
Some clients may connect slower.

[ View diagnostics ]
```

---

# 6. P0 — Carrier Monitoring

Показывать только то, что Telemt 3.5.7 действительно считает. Источник — Prometheus-метрики
`telemt_web_*` и `GET /v1/runtime/web/status`.

## Основные показатели

```text
Carrier selections · 24h
41,208

Carrier failures · 24h
142

Rejected attempts · 24h
37

Evicted sessions · 24h
23

Bridge recoveries · 24h
3
```

Каждый показатель, для которого нода не вернула данных, показывается как `Not available`,
а не как `0`.

## Carrier selection distribution

Доли выборов carrier'а за окно, из `telemt_web_carrier_selections_total{carrier,disposition}`
с `disposition="applied"`. Это распределение выборов, а не число живых сессий.

```text
WebSocket Lanes      63%
WebSocket            21%
HTTPS Lanes          11%
HTTPS                 5%
```

Формулировка «сейчас активно N сессий по carrier'у» запрещена: gauge живых сессий по
carrier'ам в 3.5.7 отсутствует.

## Источники

| Показатель | Метрика |
|---|---|
| Carrier selections | `telemt_web_carrier_selections_total{carrier,disposition}` |
| Carrier failures | `telemt_web_carrier_reported_failures_total{carrier,phase,reason}` |
| Rejected attempts | `telemt_web_rejections_total{reason}` |
| Evicted sessions | `telemt_web_session_closures_total{carrier,reason}` |
| Bridge recoveries | `telemt_web_bridge_recovery_events_total{event}` |
| Learning state | `telemt_web_carrier_learning_state{state}`, `..._entries{kind}` |

`telemt_web_carrier_reported_failures_total` документация помечает как диагностический —
подавать его как SLA-метрику нельзя.

## Графики

- carrier selection distribution во времени;
- carrier failures по carrier'ам;
- rejected attempts;
- evicted sessions;
- bridge recoveries.

Setup latency P50/P95 из P0 исключена: гистограмм в 3.5.7 нет, а латентность панели до ноды
измеряет совсем другое и не должна выдаваться за время установки клиентской сессии.

Не пытаться определять оператора связи пользователя без надёжных данных.

---

# 7. P0 — Telemt capability detection

## Проблема

Нельзя строить функции вокруг `engine == telemt`: Telemt быстро развивается.

При этом `GET /v1/system/info` **не отдаёт feature-флагов** — только
`version`, `target_arch`, `target_os`, `build_profile`, `git_commit`, `build_time_utc`,
`config_hash`, `uptime_seconds` и счётчики перезагрузки конфига.

## Требование

Capabilities вычисляет сама панель, а не получает готовыми. Источники, в порядке применения:

1. версия Telemt из `/v1/system/info`;
2. наличие и ответ конкретных эндпоинтов;
3. при необходимости — безопасная feature probe (запрос, не меняющий состояние).

В модели ноды хранить:

```text
telemt_version
telemt_build
telemt_capabilities              -- вычислено панелью
telemt_capabilities_checked_at
```

Adapter отдаёт типизированную структуру:

```go
type TelemtCapabilities struct {
    Web                  bool
    CarrierNegotiation   bool
    CarrierLearning      bool
    CarrierLearningReset  bool
    WebPause             bool
    WebDrain             bool
    WebResume            bool
    WebRuntime           bool
    HttpUpstreamDecoy    bool
    TLSEmulation         bool
    MiddleProxy          bool
}
```

UI включает функции по capability, а не по строковому сравнению версии.

Пока capabilities не вычислены, соответствующие блоки показывают `Not available`,
а не выключенное состояние: «мы ещё не знаем» и «нода не умеет» — разные вещи.

---

# 8. P0 — Safe Telemt lifecycle

Использовать WEB lifecycle Telemt:

```text
pause
drain
resume
```

## Safe maintenance workflow

```text
Pre-flight
↓
Pause new WEB sessions if required
↓
Drain
↓
Wait for active sessions / timeout
↓
Apply maintenance
↓
Restart Telemt if required
↓
Ready check
↓
WEB diagnostics
↓
Resume
```

UI:

```text
Updating Telemt

✓ Binary downloaded
✓ SHA256 verified
✓ WEB traffic draining
  18 sessions remaining
✓ Telemt restarted
✓ WEB transport healthy
✓ Fake-TLS healthy
✓ Node resumed
```

---

# 9. P0 — Update Manager

Node → Telemt:

```text
Installed
3.5.7

Recommended
3.5.7

Status
Up to date
```

При новой версии:

```text
Telemt 3.5.8 available

[ Release notes ]

[ Update ]
```

Update policy:

```text
Manual
Stable recommended
```

Prerelease по умолчанию не предлагать.

## Rollback

Перед заменой:

- сохранить старый binary;
- сохранить текущий generated config;
- проверить SHA256 нового binary.

Если health-check или WEB diagnostics после обновления не проходят:

```text
rollback binary
rollback config
restart
recheck
```

UI должен явно показать rollback.

---

# 10. P1 — Node WEB Policy

Добавить отдельную настройку на уровне ноды:

```text
Node
→ WEB Proxy
→ Transport
```

## Basic UI

```text
Transport strategy

● Automatic · Recommended
○ Maximum compatibility
○ Prefer WebSocket
○ HTTPS only
○ Custom
```

### Automatic

```text
Fallback
HTTPS

Negotiation
WebSocket Lanes
WebSocket
HTTPS Lanes
HTTPS

Carrier Learning
Enabled

Learning policy
Conservative
```

## Advanced UI

```text
Negotiation deadlines
3 / 5 / 8 / 12 s

Carrier health
30 s

Learning window
600 s

Bridge request
10 s

Bridge retry
90 s

Probe coalesce
0 ms
```

---

# 11. P1 — WEB overload protection

Поддержать WEB capacity/overload параметры Telemt.

В Advanced:

```text
Overload protection

Preset
Balanced

Connection capacity action
wait
```

Пресеты:

```text
Balanced
High load
Custom
```

Мониторить:

```text
WEB capacity
Pending sessions
Pending streams
Rejected requests
Capacity events
```

---

# 12. P1 — WEB per-profile limits

Telemt поддерживает под `[[web.vhosts.profiles]]` ровно три per-profile лимита:

```text
max_sessions
max_streams
max_streams_per_session
```

`max_pending_per_session` в Telemt **не существует** и должен быть полностью исключён из
Telemt-части: ключ не игнорируется, а валится на строгой валидации конфига и не даёт ноде
подняться. Поле остаётся только для legacy tproxy-server, у которого свой формат профилей.

## Обязательная валидация до применения

Per-profile лимит не может превышать соответствующий глобальный из `[web.limits]`
(по умолчанию `max_sessions_global = 128`, `max_streams_global = 4096`,
`max_streams_per_session = 128`). Превышение — не подрезается, а отвергается целиком.

Поэтому:

- панель хранит актуальные глобальные значения ноды и валидирует против них
  **и в API, и в UI**, до отправки конфига;
- UI показывает потолок рядом с полем и не даёт сохранить значение выше;
- ноль недопустим: лимит либо не задан, либо строго положительный.

`[web.limits]` меняется только с рестартом, поэтому поднять потолок и одновременно
задать per-profile значение выше старого потолка одним применением нельзя — панель обязана
провести это двумя шагами и честно сказать об этом оператору.

## UI

```text
Key
→ Limits
→ Advanced WEB limits
```

По умолчанию:

```text
Automatic · Recommended
```

Не заставлять пользователя вручную выставлять stream limits.

---

# 13. P1 — Decoy website modes

Сейчас есть static directory.

Добавить:

```text
Generated site
Uploaded static site
Existing website / reverse proxy
```

Для reverse proxy:

```text
Origin
http://127.0.0.1:3000

[ Test origin ]
```

Проверка:

```text
Reachable
✓

HTTP status
200

Response time
18 ms
```

Публичный vhost должен по-прежнему идти через Telemt decoy logic.

Не создавать отдельные очевидные публичные carrier routes в Caddy.

---

# 14. P1 — Fake-TLS / SNI

Добавить:

```text
Fake-TLS masking

● Use proxy hostname
○ Custom domain
```

Для custom:

```text
Mask domain
[ example.com ]

[ Test domain ]
```

SNI test:

```text
DNS
✓

TCP 443
✓

TLS handshake
✓

Certificate
✓

HTTP
200

Latency
54 ms

Suitable
✓
```

Default не менять на случайный внешний домен автоматически.

---

# 15. P1 — TLS emulation

Сделать TLS emulation явной capability/config.

В Basic:

```text
TLS emulation
Enabled · Recommended
```

Настройку изменения — только Advanced.

Diagnostics должны проверять:

- включено ли;
- работает ли;
- существует ли требуемый state/cache;
- нет ли ошибок TLS front.

---

# 16. P2 — Middle Proxy / ad_tag

Не включать по умолчанию.

Добавить Advanced:

```text
Telegram sponsor / MTProxy tag

○ Disabled
○ Enabled

ad_tag
[ ... ]
```

Поддержать:

- global tag;
- per-user tag, если поддерживает текущий adapter/capability.

---

# 17. Общие UI/UX-принципы

## 17.1. Панель должна быть task-oriented

Не:

```text
engine
carrier
tls_domain
port
streams
public_ip
```

А:

```text
Добавить сервер
Создать доступ
Выдать подключение
Проверить сервер
Понять причину проблемы
Обновить сервер
```

## 17.2. Basic / Advanced

Обычный оператор видит только то, что нужно для повседневной работы.

### Basic

```text
Server
Domain
Location
Expiration
Traffic
Speed
IP limit
WEB status
Fake-TLS status
```

### Advanced

```text
Carrier policy
Carrier learning
Negotiation timing
WEB stream limits
TLS emulation
SNI
Middle proxy
Overload policy
Runtime lifecycle
```

Использовать единый компонент:

```text
Show advanced settings
```

во всей панели.

---

# 18. UI/UX Audit — Dashboard

## Как сейчас

Dashboard уже содержит:

- KPI tiles;
- nodes online;
- active keys;
- sessions;
- traffic;
- alerts;
- nodes table;
- sessions chart;
- recent jobs.

## Проблема

Dashboard смешивает:

- состояние инфраструктуры;
- статистику;
- операции;
- историю jobs.

В результате оператору сложнее сразу понять главное:

> всё ли сейчас работает и где нужна реакция?

## Как должно быть

### Первый экран

```text
┌──────────────────────────────────────────────────────────────┐
│ Dashboard                                      [ Create key ]│
│ Updated 8 sec ago                                           │
├──────────────────────────────────────────────────────────────┤
│ ● All systems operational                                  │
│ 8 / 8 nodes online                                         │
└──────────────────────────────────────────────────────────────┘

┌────────────┬────────────┬────────────┬────────────┐
│ Nodes      │ Active     │ Sessions   │ Traffic    │
│ 8 / 8      │ keys 3104  │ 4821       │ 1.7 TB     │
└────────────┴────────────┴────────────┴────────────┘
```

### Второй блок — Problems requiring attention

Если есть проблемы:

```text
Attention

Germany #2
TLS certificate expires in 5 days
[ Fix ]

Netherlands
WebSocket carrier failures increased
142 failures during the last 15 minutes
[ Diagnose ]

Finland
Telemt update available
[ Update ]
```

Если проблем нет:

```text
✓ No issues detected
```

### Ниже

- nodes health;
- sessions chart;
- recent activity.

Recent jobs должен быть ниже operational health, а не конкурировать с ним.

---

# 19. UI/UX Audit — Keys

## Как сейчас

Keys содержит:

- список ключей;
- filters;
- create dialog;
- shared/personal/batch modes;
- node selection;
- carrier;
- expiry;
- note;
- generic limits;
- Telemt limits;
- detail drawer;
- stats;
- links dialog;
- subscription link.

## Проблема

Слишком много разных концепций показываются пользователю сразу.

Главный пользовательский сценарий:

```text
создать доступ
→ выбрать кому
→ выбрать где
→ установить срок/лимит
→ выдать человеку ссылку
```

а не конфигурировать proxy protocol.

## Как должно быть

### KeysPage

Header:

```text
Access

3,104 active

[ Search... ] [ Status ▼ ] [ Node ▼ ]

[ + Create access ]
```

Table:

```text
Name
Type
Locations
Usage
Expires
Status
Last activity
```

Не выводить низкоуровневый transport в основную таблицу.

### Create access

```text
Create access

Type
[ Personal ] [ Shared ] [ Batch ]

Name
[ Mikhail ]

Locations
☑ Germany
☑ Netherlands
☐ Finland

Expires
[ Never ▼ ]

Limits
Standard · No limits       [ Change ]
```

Summary:

```text
Personal access
2 locations
WEB + Fake-TLS
No expiration
No limits

[ Create access ]
```

### Limits

Открывать отдельным sheet:

```text
Traffic
Unlimited

Speed
Unlimited

Devices / unique IPs
Unlimited

Connections
Automatic

Advanced WEB limits
Automatic
```

### После создания

Сразу показывать выдачу доступа:

```text
Access created

Mikhail

[ Copy subscription link ]
[ Show QR ]
[ Open in Telegram ]

Individual servers
...
```

---

# 20. UI/UX Audit — Key Detail

## Как сейчас

KeyDetailDrawer выполняет одновременно:

- просмотр;
- редактирование;
- limits;
- stats;
- actions.

## Проблема

Drawer становится слишком тяжёлым и длинным.

## Как должно быть

Верх:

```text
Mikhail                       Active

Personal access

2 locations
Germany
Netherlands
```

Quick actions:

```text
[ Connection ]
[ Edit ]
[ Disable ]
```

Stats:

```text
Traffic
28.4 GB

Active sessions
3

Unique IPs
2

Last activity
2 min ago
```

Sections:

```text
Overview
Usage
Limits
Activity
```

Опасные действия — в отдельном Danger zone.

---

# 21. UI/UX Audit — Connection / Link Dialog

## Как сейчас

QR, `t.me`, `tg://`, node groups, WEB/Fake-TLS tabs и subscription link визуально имеют примерно одинаковую важность.

## Проблема

Оператор обычно хочет просто дать пользователю подключение.

## Как должно быть

Главное:

```text
Mikhail

Ready on 3 locations

[ Copy subscription link ]
[ Show QR ]
```

Дальше:

```text
Individual servers

Germany                     Online

[ WEB Proxy ] [ Fake-TLS ]

[ Open in Telegram ]
[ Copy link ]

Advanced
t.me/...
tg://...
```

Низкоуровневые URI свернуть в Advanced.

---

# 22. UI/UX Audit — Nodes list

## Как сейчас

Nodes — технический список серверов с engine/status/load и действиями.

## Проблема

Статус ноды должен говорить не только:

```text
online/offline
```

но и:

```text
Telegram Proxy реально работает / деградировал / сломан
```

## Как должно быть

Table:

```text
Server
Proxy status
Connections
Traffic
CPU
RAM
Version
Updated
```

Пример:

```text
Germany #1
de1.example.com

● Healthy
WEB + Fake-TLS

641
42 Mbps
34%
48%
3.5.7
8 sec ago
```

Degraded:

```text
● Degraded
WEB fallback high

[ Diagnose ]
```

На мобильном — карточки.

---

# 23. UI/UX Audit — Create Node

## Как сейчас

Одна scrollable форма:

- name;
- hostname;
- engine;
- TLS domain;
- classic port;
- ACME e-mail;
- public IP.

## Проблема

Обычный пользователь видит детали реализации раньше, чем понимает основной сценарий.

## Как должно быть

Wizard из 3 этапов.

### Step 1 — Server

```text
Add proxy server

Name
[ Germany · Hetzner ]

Domain
[ de1.example.com ]

Server IP
[ 95.x.x.x ]
```

После ввода domain/IP:

```text
DNS check

✓ de1.example.com → 95.x.x.x
```

Если DNS неверный:

```text
DNS record required

Type   A
Name   de1.example.com
Value  95.x.x.x

[ Copy ]
```

### Step 2 — Proxy

```text
Proxy engine

● Telemt
  Recommended
  WEB + Fake-TLS
  limits
  statistics

○ Legacy tproxy
```

Для Telemt:

```text
WEB Proxy
Enabled

Fake-TLS
Enabled

WEB port
443

Fake-TLS port
8443

Transport
Automatic
```

`tls_domain` убрать из Basic.

### Step 3 — Install

```text
Install TGProxy Node

1. Connect to the server as root
2. Run command

┌───────────────────────────────┐
│ curl ...                      │
└───────────────────────────────┘
                       [ Copy ]

Waiting for server...
```

После подключения:

```text
✓ Agent connected
✓ Telemt installed
✓ Caddy configured
✓ TLS issued
✓ WEB Proxy healthy
✓ Fake-TLS healthy

[ Open server ]
```

---

# 24. UI/UX Audit — Node Detail

## Как сейчас

Node Detail разбит на технические cards/tabs: listeners, DC latency, logs, stats, checks и т.д.

## Проблема

Информация есть, но оператору приходится собирать ответ самостоятельно.

Главные вопросы:

- сервер работает?
- proxy работает?
- есть ли пользователи?
- есть ли проблема?
- что именно сломалось?

## Как должно быть

Header:

```text
Germany · Hetzner                         ● Healthy

de1.example.com
95.x.x.x

WEB Proxy        ● Online
Fake-TLS         ● Online
Telemt 3.5.7     ● Current
```

Stats:

```text
238
Active users

641
Connections

42 Mbps
Traffic

34%
CPU
```

Tabs:

```text
Overview
WEB Transport
Connections
Performance
Website
Logs
Settings
```

### Overview

Показывает:

- service health;
- WEB status;
- Fake-TLS status;
- TLS/certificate;
- traffic;
- active sessions;
- CPU/RAM/disk;
- current version;
- текущие проблемы.

### Advanced technical cards

Listeners/DC internals не удалять, но перенести в:

```text
Settings / Diagnostics / Advanced
```

---

# 25. UI/UX Audit — WEB Transport tab

Новый отдельный tab.

```text
WEB Proxy

Status
Healthy

Port
443

TLS
Valid · 47 days

Negotiation
Automatic

Carrier learning
Active
```

## Carrier Learning

```text
Carrier Learning                         Healthy

Learning             Enabled
Negotiation          Conservative

Carrier selections · last 24h
WebSocket Lanes      63%
WebSocket            21%
HTTPS Lanes          11%
HTTPS                 5%

WebSocket failures   18
Bridge recoveries     3

[ Reset learning ]   [ Diagnostics ]
```

Подписи означают ровно то, что считает Telemt: доли **выборов** carrier'а за окно, а не
число живых сессий, и события восстановления моста, а не повторы.

## Показатели

```text
Carrier selections · 24h
Carrier failures · 24h
Rejected attempts · 24h
Evicted sessions · 24h
Bridge recoveries · 24h
```

Любой из них, по которому нода не отдала данных, рендерится как `Not available`.

Setup latency P50/P95 на этой вкладке отсутствует: в 3.5.7 её измерить нечем.

CTA:

```text
[ Run transport diagnostics ]
```

---

# 26. UI/UX Audit — Monitoring

## Как сейчас

Monitoring в основном построен вокруг графиков и series.

## Проблема

Это ближе к облегчённой Grafana.

Для оператора полезнее сначала увидеть:

```text
что сломалось?
где?
с какого момента?
какое влияние?
```

## Как должно быть

Tabs:

```text
Overview
Problems
Nodes
WEB Transport
```

### Overview

```text
Fleet health

8 / 8 nodes online
7 healthy
1 degraded

Carrier failures · 24h
142

Sessions
4,821

Traffic
42 Mbps
```

### Problems

```text
Netherlands · WEB transport degraded

Started
14:32

Impact
WebSocket Lanes reporting failures.
Clients are being served on other carriers.

WebSocket Lanes failures
142 during the last 15 minutes

Recommended action
Run WEB diagnostics.

[ Diagnose ]
```

Другие alerts:

- node offline;
- Telemt offline;
- Caddy offline;
- certificate expiring;
- DNS mismatch;
- public IP mismatch;
- bridge recovery events;
- carrier failures;
- rejected attempts;
- evicted sessions;
- disk high;
- CPU/RAM high;
- outdated Telemt;
- decoy unavailable.

### WEB Transport

Fleet-level carrier distribution и failures.

---

# 27. UI/UX Audit — Sites

## Как сейчас

Есть:

- templates list;
- template editor;
- line-numbered textarea;
- preview;
- assign template dialog.

## Проблема

Концепция ориентирована на «редактирование шаблона», а не на задачу:

```text
какой сайт сейчас видит посетитель этого proxy domain?
```

## Как должно быть

Переименовать UX-концепцию в `Websites`.

Основной экран:

```text
Websites

Templates
3

Assigned nodes
8
```

Cards:

```text
Default landing

Static website
Used by 5 nodes

[ Preview ]
[ Edit ]
[ Assign ]
```

### Editor

Layout desktop:

```text
┌──────────────────────┬───────────────────────┐
│ Editor               │ Live preview          │
│                      │                       │
│ HTML/CSS             │ example rendering     │
│                      │                       │
└──────────────────────┴───────────────────────┘
```

Toolbar:

```text
Desktop
Tablet
Mobile

[ Open preview ]
[ Save ]
```

Нужны:

- unsaved indicator;
- autosave draft;
- clear preview error;
- syntax error state;
- reset to template;
- duplicate template.

### Assign

Вместо большого dialog:

```text
Assign website

Template
Default landing

Nodes

☑ Germany
☑ Netherlands
☐ Finland

[ Assign ]
```

Для node detail должна быть ссылка:

```text
Website
Default landing
[ Preview ] [ Change ]
```

---

# 28. UI/UX Audit — Settings

## Как сейчас

Settings содержит множество крупных форм:

- Panel;
- Preferences;
- Security;
- TOTP;
- Admins;
- Branding;
- Branding Profiles;
- Backups;
- Telegram и другие параметры.

## Проблема

Один Settings screen быстро превращается в длинную административную страницу.

Сложно:

- найти нужную настройку;
- понять, что изменено;
- понять, что требует Save;
- отличить глобальные настройки от личных.

## Как должно быть

Левое внутреннее меню:

```text
Settings

General
Appearance
Telegram
Security
Administrators
Backups
Advanced
```

### General

```text
Panel name
Public URL
Language defaults
Timezone
```

### Appearance

```text
Branding
Logo
Primary color
Accent
Login page
Subscription page

Branding profiles
```

### Telegram

```text
Bot
Notifications
Integration status
```

### Security

```text
Password
2FA
Sessions
Security policy
```

### Administrators

Table:

```text
Admin
Role
2FA
Last login
Status
```

### Backups

Не только form.

Сделать operational screen:

```text
Backups

Automatic backups
Enabled

Last successful backup
Today 03:00

Next backup
Tomorrow 03:00

Storage
S3

[ Run backup now ]
```

History:

```text
Today 03:00      Success
Yesterday 03:00  Success
Sep 7 03:00      Failed
```

### Save UX

Формы с изменениями должны показывать sticky bottom bar:

```text
Unsaved changes

[ Discard ] [ Save changes ]
```

Не держать Save button далеко внизу большой формы.

---

# 29. UI/UX — Empty, Loading, Error и Degraded состояния

Для каждой основной страницы предусмотреть 5 состояний:

```text
Loading
Empty
Error
Degraded
Not available
```

`Not available` — отдельное состояние, а не разновидность пустого. Оно означает, что
источник данных недоступен или возможность не поддерживается этой версией Telemt.

Показывать `0` вместо недоступного значения запрещено во всей панели: ноль читается как
измерение и вводит оператора в заблуждение сильнее, чем честное «нет данных».

Не использовать одинаковый generic error для всего.

Пример:

```text
WEB diagnostics unavailable

Telemt API is reachable, but WEB runtime did not respond.

[ Retry ]
[ Open logs ]
```

---

# 30. UI/UX — Status system

Ввести единый status vocabulary.

```text
Healthy
Degraded
Offline
Installing
Updating
Draining
Unknown
```

Не смешивать:

```text
online
active
ok
ready
connected
```

без необходимости.

Цвет + icon + текст.

Нельзя передавать статус только цветом.

---

# 31. UI/UX — Actions hierarchy

На каждом экране:

- максимум одна Primary CTA;
- destructive отдельно;
- secondary действия через menu;
- технические actions не конкурируют с пользовательскими.

Пример Node:

```text
Primary
[ Diagnose ]

Secondary
Restart
Update
Edit

Danger
Delete node
```

---

# 32. UI/UX — Help

Существующую contextual help сохранить и расширить.

Help должен объяснять:

- что это;
- зачем;
- default;
- когда менять;
- пример;
- риск неправильной настройки.

Особенно для:

```text
Carrier Learning
TLS Emulation
SNI
Unique IP limit
WEB stream limits
Middle Proxy
Overload policy
```

---

# 33. Backend model — WEB policy

Рекомендуется:

```text
nodes.telemt_web_policy JSONB
```

Пример:

```json
{
  "preset": "automatic",
  "carrier": "https",
  "carriers": [
    "websocket-lanes",
    "websocket",
    "https-lanes"
  ],
  "carrier_learning": true,
  "carrier_negotiation_aggressiveness": "conservative",
  "timeouts": {
    "carrier_negotiation_deadlines_secs": [3, 5, 8, 12],
    "carrier_health_secs": 30,
    "carrier_learning_secs": 600,
    "bridge_request_secs": 10,
    "bridge_retry_secs": 90,
    "carrier_probe_coalesce_ms": 0
  }
}
```

Отдельными колонками:

```text
telemt_version
telemt_build
telemt_update_available
telemt_capabilities
```

---

# 34. Backend model — monitoring

Снапшоты пишутся только из реально существующих счётчиков. Все поля — nullable:
отсутствие данных хранится как `NULL` и доходит до UI как `null`, никогда как `0`.

Накопительные счётчики Telemt монотонны и обнуляются при рестарте процесса, поэтому
панель хранит сырые значения и считает дельты, отбрасывая интервал, в котором счётчик
уменьшился (это рестарт, а не отрицательный трафик).

```text
web_carrier_selections_https
web_carrier_selections_https_lanes
web_carrier_selections_websocket
web_carrier_selections_websocket_lanes

web_carrier_failures
web_rejected_attempts
web_evicted_sessions
web_bridge_recoveries

web_learning_entries
```

Полей setup latency нет — измерять нечем.

Поля вида «активные сессии по carrier'у» не заводятся: gauge в 3.5.7 отсутствует, а
считать их из дельт счётчиков означало бы выдавать оценку за факт.

Не писать credentials и bootstrap-токены.

---

# 35. Telemt Adapter

Все функции — через единый adapter. UI и API-слой не должны знать сырые эндпоинты Telemt.

```go
GetSystemInfo()          // версия, build, runtime — без feature-флагов
GetCapabilities()        // вычисляется панелью, см. §7

GetWebStatus()           // /v1/runtime/web/status: runtime_instance, lifecycle, learning
GetWebSessions()         // постранично, с фильтром по carrier

PauseWeb()
DrainWeb()               // 202, не ждёт; прогресс — через GetWebStatus
ResumeWeb()

GetWebPolicy()
PatchWebPolicy()         // возвращает, что отложено до рестарта

ResetCarrierLearning()

CollectWebMetrics()      // telemt_web_* из Prometheus-эндпоинта
```

Обязательные свойства адаптера:

- `runtime_instance` для всех `POST /v1/runtime/web/*` адаптер получает и подставляет сам;
  `409 web_runtime_mismatch` означает рестарт Telemt — инстанс перечитывается,
  запрос не повторяется вслепую;
- `PatchWebPolicy` возвращает вызывающему `deferred_process_fields` /
  `process_restart_required`; молча считать патч применённым нельзя;
- недоступная возможность отдаётся как «неизвестно/не поддерживается», а не нулевым значением;
- адаптер не логирует секреты: ни секреты пользователей, ни api-токен Telemt.

---

# 36. Configuration apply

Использовать hot apply там, где Telemt это позволяет.

Workflow:

```text
Patch config
↓
Reload
↓
Verify
```

Если restart обязателен:

```json
{
  "apply_mode": "restart_required"
}
```

UI:

```text
Restart required

This change requires restarting Telemt.
37 active sessions may reconnect.

[ Cancel ]
[ Apply and restart ]
```

---

# 37. Diagnostics history

Каждый manual/automatic diagnostics run сохранять.

```text
node_id
started_at
finished_at
overall_status
checks JSONB
trigger
```

Trigger:

```text
manual
scheduled
post_install
post_update
alert
```

В Node Detail:

```text
Diagnostics history

Today 15:32   Healthy
Today 11:00   Degraded
Yesterday     Healthy
```

---

# 38. Automatic health checks

Не запускать тяжёлый полный diagnostics каждую минуту.

Разделить:

## Lightweight

каждые 1–5 минут:

- agent;
- Telemt ready;
- Caddy;
- TCP;
- current runtime counters.

## Deep diagnostics

- по кнопке;
- после install;
- после update;
- при переходе Healthy → Degraded;
- периодически, например раз в несколько часов.

---

# 39. Node installation pre-flight

Перед установкой/инициализацией проверять:

```text
OS
Architecture
Root privileges
systemd
Free ports
Disk space
Memory
DNS
Public IP
Outbound HTTPS
Telegram DC connectivity
Caddy compatibility
Existing conflicting services
```

UI:

```text
Server pre-flight

Ubuntu 22.04            ✓
amd64                   ✓
Port 443                ✓
DNS                     ✓
Outbound network        ✓
Telegram connectivity   ✓

Ready to install
```

---

# 40. Alerting

Добавить alert rules:

```text
Node offline > N min
Carrier failures > N per window
Rejected attempts > N per window
Evicted sessions > N per window
Certificate expires < N days
Disk > X%
CPU > X% for N min
Telemt version outdated
Deep diagnostics failed
Decoy unavailable
```

Alert должен содержать:

```text
Problem
Impact
Started
Current value
Recommended action
```

Правило порога: алерты строятся на абсолютных счётчиках за окно
(«142 отказа за 15 минут»), а не на процентах, знаменателя для которых у нас нет.

Отсутствие метрики — не повод для алерта. Если нода не отдала счётчик, состояние
`Not available`, а не «0, значит всё хорошо» и не «упало до нуля».

---

# 41. Acceptance tests — WEB

Проверить:

```text
Telegram Desktop Windows
Telegram Desktop macOS
Telegram Android
Telegram iOS
```

Важно про iOS: клиенты без метаданных (в том числе Telegram iOS) всегда используют
фиксированный `carrier` и поддерживают только `https`. Это не деградация и не fallback —
negotiation к ним просто не применяется, и UI не должен показывать это как проблему.

Для negotiation-capable клиентов:

- разные carrier;
- fallback;
- reconnect;
- server restart.

Для iOS отдельно проверить HTTPS fallback.

---

# 42. Acceptance tests — restart/update

Создать WEB sessions разных типов.

Проверить:

```text
drain
restart
ready
WEB reconnect
no stale sessions
new connections healthy
```

Update:

```text
download
sha256
drain
replace
restart
diagnostics
resume
```

Failure:

```text
rollback
restart
diagnostics
```

---

# 43. Acceptance tests — UX

Обязательные сценарии Playwright:

### Create node

```text
Add server
→ fill basics
→ install
→ live connection state
→ open node
```

### Create key

```text
Create access
→ Telemt only
→ carrier field absent
→ create
→ subscription link visible
```

### Mixed Telemt/tproxy

```text
Create access
→ select mixed nodes
→ legacy carrier visible
→ explanatory hint visible
```

### Diagnostics

```text
Run diagnostics
→ progress
→ structured results
→ degraded check
→ suggested action
```

### Update

```text
Update Telemt
→ drain progress
→ restart
→ diagnostics
→ success
```

---

# 44. Приоритет реализации

## P0

1. Carrier Learning.
2. Capability/version detection.
3. Удаление carrier из Telemt key workflow.
4. WEB diagnostics backend.
5. WEB diagnostics UI.
6. WEB carrier/runtime monitoring.
7. Safe Telemt lifecycle.
8. Safe update + rollback.
9. Create Node wizard.
10. Simplified Create Access.
11. Node Detail information architecture.
12. Dashboard problems-first redesign.

## P1

13. WEB Transport tab.
14. Node WEB Policy presets.
15. Overload protection.
16. WEB profile limits.
17. Reverse-proxy decoy.
18. SNI checker.
19. TLS emulation diagnostics.
20. Monitoring Problems tab.
21. Key Detail redesign.
22. Connection dialog redesign.
23. Sites/Websites redesign.
24. Settings information architecture redesign.

## P2

25. Middle Proxy / ad_tag.
26. Server optimisation audit.
27. Carrier learning debug/reset.
28. Deep diagnostics export.
29. Recommended-version notifications.

---

# 45. Итоговый основной сценарий

Обычный оператор должен уметь сделать всё так:

```text
Add Server
↓
Germany
de1.example.com
95.x.x.x
↓
Telemt · Recommended
↓
Install
↓
✓ Agent connected
✓ Telemt
✓ Caddy
✓ TLS
✓ WEB Proxy
✓ Fake-TLS
↓
Create Access
↓
Mikhail
Germany + Netherlands
30 days
↓
Create
↓
[ Copy subscription link ]
[ Show QR ]
```

Параметры вроде:

```text
websocket-lanes
carrier_learning_secs
tls_front_dir
max_streams_per_session
bridge_retry_secs
http_connection_capacity_action
```

должны оставаться доступными Advanced-пользователю, но не мешать обычной ежедневной работе.

---

# 46. Главный UX-принцип проекта

TGProxy Panel должен выглядеть и ощущаться не как GUI над `telemt.toml`, а как полноценный продукт управления Telegram Proxy инфраструктурой.

Пользователь должен видеть:

```text
что работает
что не работает
кого это затрагивает
что сделать дальше
```

а детали протокола — только тогда, когда они действительно нужны.
