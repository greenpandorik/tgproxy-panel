# Связь ноды с датацентрами Telegram (фаза 10)

Дата: 2026-09-08. Запрос заказчика: «видеть, как та или иная нода работает с DC Telegram».

## Источник данных

telemt держит по каждому DC скользящую среднюю задержки (EMA), которую обновляют его же
проверки здоровья, и отдаёт её через Control API `GET /v1/stats/upstreams` (проверено на
боевой ноде 3.5.7). Нужные поля:

```
data.enabled                          bool
data.zero.connect_success_total       int64   всего удачных соединений к Telegram
data.zero.connect_fail_total          int64
data.upstreams[0].route_kind          "direct" (у нас всегда прямой маршрут)
data.upstreams[0].healthy             bool
data.upstreams[0].fails               int
data.upstreams[0].last_check_age_secs int
data.upstreams[0].effective_latency_ms float
data.upstreams[0].dc[]                {dc:int, latency_ema_ms:float|null, ip_preference:string}
```

`GET /v1/stats/dcs` отдаёт пустой список вне режима middle-proxy, его не используем.
На tproxy-нодах данных нет: панель показывает «недоступно на этом движке».

## Контракт агент → панель (proto, обратно совместимо)

```
message DcLatency { int32 dc = 1; double latency_ms = 2; bool known = 3; string ip_preference = 4; }
HealthReport +=
  repeated DcLatency dcs = 13;
  bool   upstream_healthy = 14;
  int32  upstream_fails = 15;
  double effective_latency_ms = 16;
  int64  connect_success_total = 17;
  int64  connect_fail_total = 18;
  int64  upstream_last_check_age_secs = 19;
  bool   dc_data_available = 20;   // false на tproxy и когда telemt вернул enabled=false
```

Агент вызывает `/v1/stats/upstreams` в том же цикле heartbeat, что и остальное здоровье;
ошибка вызова не ломает heartbeat, а даёт `dc_data_available=false`.

## Хранение и API панели

- `nodes.last_health` (JSON) получает те же поля в snake_case: `dcs`, `upstream_healthy`,
  `upstream_fails`, `effective_latency_ms`, `connect_success_total`, `connect_fail_total`,
  `upstream_last_check_age_secs`, `dc_data_available`.
- Миграция `00009_dc_latency.sql`: `node_stats_snapshots.dc_latency jsonb NOT NULL DEFAULT '{}'`
  вида `{"1": 197.9, "2": 36.5, ...}` (ключ = номер DC, значение = мс; неизвестные DC
  пропускаются). Воркер статистики берёт значения из `last_health` при снимке.
- `GET /api/v1/monitoring/nodes/{id}/series` и агрегированный обзор отдают `dc_latency` в
  каждой точке (в бакете среднее по DC).
- `GET /api/v1/nodes` и `/nodes/{id}`: `health` уже нормализуется через `healthJSON`, новые
  поля едут там же.

## Тон задержки (одна чистая функция, с тестом)

| Задержка до DC | Тон |
|---|---|
| неизвестна / нет данных | neutral |
| < 150 мс | ok |
| 150 – 400 мс | warn |
| ≥ 400 мс | err |

Маршрут: `healthy=false` → err; `fails>0` при `healthy=true` → warn; иначе ok.

## Интерфейс

1. **Карточка ноды → вкладка «Обзор»**: панель «Датацентры Telegram» (значок Globe).
   Шапка: «маршрут прямой · соединений 58 из 58 · проверка 29 с назад», тон по маршруту.
   Тело: строки DC1…DCn: номер (mono), задержка (mono, тон по таблице), предпочтение IP.
   Пусто/недоступно/ошибка по существующему шаблону состояний. На tproxy: «недоступно на
   этом движке».
2. **Карточка ноды → «Статистика»**: график «Задержка до датацентров», линия на DC, тот же
   переключатель диапазона, цвета серий из токенов, оси mono.
3. **Список нод**: колонка «Telegram» = `effective_latency_ms` (mono, тон), прочерк без
   данных. Данные из `last_health`, без дополнительных запросов.
4. **Дашборд**: двенадцатая плитка «Задержка до Telegram» = среднее `effective_latency_ms`
   по онлайн-нодам, тон по таблице; с ней сетка 4×3 без хвоста. Значок Globe.

## Границы

Маршруты, существующие ключи i18n, поля форм не меняются; новые ключи ru/en с паритетом.
Шкалы фазы 8 и правила тона фазы 9 обязательны. Контраст меряется на отрисованной странице.
Приёмка: Go-набор с `-race`, `make e2e-telemt` (настоящий telemt отдаёт настоящий JSON), web-набор,
скриншоты панели «Датацентры Telegram» и графика в документацию, справка (`help`) для панели.
