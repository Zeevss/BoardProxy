# BoardProxy Control Plane — выполненные этапы

## Архитектурные инварианты

- core — disposable runtime из TOML/stdin, без БД и business history;
- node-agent хранит только сертификат, checkpoints и ограниченный telemetry outbox в SQLite;
- Kotlin control-plane — единственный владелец desired state и истории;
- browser HTTP/SSE и node mTLS gRPC — разные контракты;
- изменение создаёт config revision только когда поменялись байты TOML;
- interface traffic и per-user payload никогда не смешиваются.

## 1. Granular management и история — выполнено

- отдельные commands для node, board, user и assignment;
- `ETag`/`If-Match`, pagination/filtering fleet списка;
- encrypted source snapshots, history, safe diff без private keys;
- rollback создаёт новую монотонную версию, не переписывает прошлое;
- изменение создаёт revision, audit и outbox в одной транзакции.

## 2. Traffic analytics и limits — выполнено

- раздельные interface/user series с настраиваемым bucket;
- идемпотентные hourly rollups;
- retention: raw 31 день и rollups 730 дней по умолчанию;
- daily/monthly per-user quotas;
- безопасный default `alert`, явный `disable` меняет desired user state;
- локальный node outbox ограничен по байтам; при backpressure checkpoint не продвигается.

## 3. Fleet и security hardening — выполнено

- one-time enrollment, локальная private key ноды, CSR и 30-дневный сертификат;
- TLS 1.3/mTLS, fingerprint inventory, revocation и emergency node disable;
- API token hashes, roles и `lastUsedAt`;
- request body limit для declared и streamed/chunked тела;
- per-token/IP rate limit и explicit CORS allowlist;
- AES-GCM keyring с несколькими master keys и active key id;
- private credentials остаются write-only и не попадают в diff/API/events.

## 4. HA и reliable delivery — выполнено

- PostgreSQL row lock сериализует компиляцию desired config одной node;
- persistent `boot_id` history и `seq` блокируют late agent writes;
- outbox использует row lock и backoff, после 10 ошибок — dead letter;
- admin API показывает dead letters и явно возвращает их в retry;
- stale online status автоматически истекает;
- replicas получают desired/runtime/status через PostgreSQL `LISTEN/NOTIFY`.

## 5. Production frontend — в работе

- React/TypeScript dashboard внутри production Spring Boot image;
- overview, node drift, runtime sessions/boards, activity и оба traffic вида;
- users/boards add, enable/disable/remove и node enable/disable;
- authenticated fetch SSE без polling;
- bearer token хранится только в `sessionStorage`;
- desktop/mobile layout и оставшиеся live API сценарии проходят текущую разработку.

## 6. Verification и recovery — выполнено

- unit/application/architecture/migration suites;
- Testcontainers contract на PostgreSQL 18 для Flyway V1–V5, config locking и boot fencing;
- runtime rebuild из authoritative snapshot и следующих decoded facts;
- Go test suites core и node-agent;
- frontend test/lint/build по мере завершения текущей реализации;
- multi-stage production image build;
- GitHub Actions для Go, Kotlin/PostgreSQL, frontend и Docker image.

## Следующая эксплуатационная работа

Это не незакрытые части шести этапов, а production rollout процедуры:

- настроить backup/restore drill и алерты под конкретную инфраструктуру;
- провести soak/load test на целевом количестве nodes/users;
- выполнить rolling-upgrade/chaos drill в staging;
- после фактических объёмов выбрать PostgreSQL partitioning или ClickHouse;
- расширить UI для history/diff и dead-letter operations — backend endpoints уже готовы.

## 7. Флотовая панель — выполнено

- пользователь стал сущностью control-plane: `GET /api/v1/users` отдаёт размещения
  по нодам, лимиты устройств/страниц и агрегированный трафик;
- `GET /api/v1/boards` отдаёт борды всего флота; селектор ноды из панели убран;
- лимит трафика управляется отдельным `/api/v1/users/{id}/quota` с собственным ETag;
- квоты расширены недельным периодом, режимом «без сброса» и политикой `reset`
  со счётчиком `counter_start`;
- подписка получила постоянную ссылку: токен и recovery-ключ хранятся
  зашифрованными мастер-ключом, `GET /api/v1/subscriptions/{id}/link` и
  `POST /{id}/rotate`;
- `GET /api/v1/nodes/{nodeId}/config` показывает применённый TOML без приватных
  ключей и клиентских идентичностей;
- панель: Настройки с гейтингом сервиса подписок, флотовые Пользователи,
  Борды по нодам, форма ноды с параметрами core и одноразовым секретом.
