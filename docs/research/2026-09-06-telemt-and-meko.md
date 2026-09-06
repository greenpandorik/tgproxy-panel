# Исследование: telemt и MTPROTO_FIX_By_MEKO

Дата: 2026-09-06. Источники: клоны репозиториев на момент коммитов telemt 2026-08-27 (v3.5.5) и MEKO 2026-08-30 (v1.99), их документация и страницы релизов.

## telemt (github.com/telemt/telemt)

Что это: MTProxy-сервер на Rust (tokio), 1872 коммита с 2025-12-30, ~170 тыс. строк, релизы почти ежедневно (3.5.0…3.5.5 за 22–27 августа 2026). Лицензия собственная TELEMT PL 3 (разрешает использование, копирование, распространение и продажу при сохранении текста лицензии). Установка: `install.sh` кладёт бинарь в `/opt/telemt`, конфиг в `/etc/telemt/telemt.toml`, systemd-юнит `telemt`; готовые бинари в GitHub Releases (`telemt-x86_64-linux-gnu.tar.gz`), Docker-образы.

Режимы: classic, secure (`dd`), Fake-TLS (`ee`, SNI-фронтинг с прозрачным TCP-сплайсом на реальный сайт: DPI видит настоящий TLS-хендшейк и сертификат домена), и **WEB-режим** (с 3.5.1): тот же протокол, что у `tproxy-server` (пути `/api/v1/up|down|ws`, subprotocol `tproxy-v1.<token>`, carrier `https`, `https-lanes`, `websocket`, `websocket-lanes`), совместимый с Telegram Desktop WEB, `dd`/plain секреты, decoy-сайт (upstream или статическая директория), серверная автонегоциация carrier. TLS терминирует NGINX/HAProxy (в нашем случае Caddy) и проксирует plain HTTP на loopback-листенер telemt.

Модель пользователей: `[access.users]` username → 32-hex секрет; на пользователя: `enabled`, `ad_tag`, `max_tcp_conns`, `expirations` (RFC3339), `data_quota` (байты), `rate_limits` (bps up/down), `max_unique_ips`, `source_deny`. Всё это горячо перезагружается без рестарта.

Управление: HTTP Control API на loopback (`[server.api] listen = "127.0.0.1:9091"`, whitelist CIDR): `GET/POST /v1/users`, `PATCH /v1/users/{u}`, `DELETE`, `rotate-secret`, `enable`, `disable`, `reset-quota`; `GET/PATCH /v1/config` (секции general, timeouts, censorship, upstreams, dc_overrides, web, server.listeners), `POST /v1/system/reload`; `GET /v1/stats/summary`, `/v1/stats/users`, `/v1/runtime/connections/summary`, `/v1/runtime/web/sessions`, health/ready. Prometheus `/metrics` на отдельном порту с whitelist (`telemt_connections_total`, `telemt_handshake_failures_by_class_total`, per-user данные через API).

Встроенный аналог фикса MEKO: `synlimit = "iptables"|"nftables"` на листенере с параметрами `synlimit_seconds/hitcount/burst` и отдельным iOS-классификатором (`synlimit_ios_*`, длина пакета 64 и TTL < 65); в README MEKO сказано, что telemt и MTProto.zig взяли их v2-фикс.

Что важно для панели: один бинарь закрывает и классический MTProxy, и Fake-TLS, и WEB-режим; есть нормальный API вместо перезаписи файлов с рестартом; лимиты и квоты на пользователя; статистика по пользователю; средний прокси (`use_middle_proxy`, `ad_tag`) для спонсорского канала. WEB-профили (`[[web.vhosts.profiles]]`: `user`, `secret_mode`, лимиты) правятся через `PATCH /v1/config` с горячей перезагрузкой; листенеры и `web.limits` требуют рестарта.

Оговорки: проект молодой и очень быстро меняется (breaking-изменения между минорными версиями, WEB-режим появился 22 августа, релизы 3.5.2–3.5.5 чинили WEB под Windows и iOS); документация огромная и местами опережает бинарные релизы («WEB mode is implemented in the current source tree… verify that packages contain the same revision»); лицензия нестандартная, но разрешительная.

## MTPROTO_FIX_By_MEKO (github.com/Mekotofeuka/MTPROTO_FIX_By_MEKO)

Что это: bash-менеджер (`mekopr`) с меню: установка telemt / MTG / MTProto.zig / python MTProtoProxy / Erlang, «фикс», оптимизация сервера, SNI-чекер, установка nginx для WEB-режима telemt, установка сторонней Telemt Panel (amirotin), 3x-ui, простой node manager по SSH. 838 коммитов с 2026-06-23, 1.3k звёзд, активный Telegram-чат.

Сам «фикс» это правила netfilter на порт прокси (`data/rules.sh`), а не патч прокси:
- v2: SYN-пакеты длиной 64 и TTL < 65 считаются iOS и принимаются без лимита; остальные SYN ограничиваются `hashlimit` 54/мин на IP (burst 1), лишние отклоняются TCP RST.
- v3: тот же двухуровневый лимит без TTL-разделения (по их словам универсальнее), варианты для iptables и nftables (`meter … limit rate 54/minute burst 1`, `1/second` для classic), отдельная цепочка `MTPR_SYNFIX`, systemd-юнит `mtpr-synfix` для восстановления после перезагрузки.
- v4 «zapret2»: обёртка над zapret2 (DPI-обход на уровне пакетов).
Плюс sysctl-оптимизации и отключение MSS-клампа (он режет скорость медиа). Проблема, которую это лечит («с 4 июня» клиенты не подключаются / бесконечное обновление на iOS / медиа не грузятся), связана с поведением сетей при повторных SYN от клиента; ограничение частоты SYN на IP заставляет клиент держать одно соединение.

Что важно для панели: логика фикса маленькая и воспроизводимая (десяток правил nftables); telemt уже несёт её внутри (`synlimit`), так что при переходе на telemt отдельный фикс не нужен, достаточно включить опцию. Сам MEKO как продукт это интерактивный bash-установщик на одну машину; наша панель покрывает ту же нишу централизованно.

## Выводы для проекта

1. Официальный MTProxy (C, 2018, редкие правки) действительно хуже telemt по управляемости: нет API, нет лимитов на пользователя, секреты только через флаг `-S` с рестартом. Наша текущая архитектура (агент переписывает `profiles.json` и `mtproxy.env`, рестартит оба сервиса, сессии рвутся) существует именно из-за этих ограничений.
2. telemt в WEB-режиме заменяет связку `tproxy-server` + MTProxy одним процессом с горячей перезагрузкой пользователей и профилей. Ключ = пользователь telemt; создание, отзыв, ротация, срок, квота, лимит IP делаются одним HTTP-вызовом на loopback без рестарта.
3. telemt даёт то, чего у нас не было принципиально: статистику и лимиты по ключу.
4. Один и тот же пользователь telemt может одновременно выдаваться как WEB-ссылка (`tg://webproxy`) и как классическая/Fake-TLS ссылка (`tg://proxy?server&port&secret`), если на ноде включить второй листенер на другом порту. Это и есть «управлять всеми прокси Telegram».
5. Риск: скорость изменений telemt. Нужна закреплённая версия (как сейчас с коммитом tproxy-server) и адаптер, изолирующий панель от их API/конфига.
