# План развития BoardProxy Control Plane

## Инварианты архитектуры

- core остаётся disposable runtime без БД и истории;
- node-agent хранит только локальное операционное состояние в SQLite;
- control-plane является единственным владельцем business desired state;
- node gRPC и browser API остаются разными контрактами;
- изменение пользователя или доски создаёт новую immutable config revision;
- интерфейсный и пользовательский трафик хранятся как разные метрики.

## Текущая точка

Работают enrollment, mTLS node stream, доставка целого TOML snapshot,
ApplyResult и надёжная telemetry outbox. Control-plane владеет нормализованным
каталогом, компилирует его в immutable TOML revisions и ведёт read model
состояния ноды. Однопроцессный filesystem adapter оставлен только для
development; frontend не начат намеренно.

Node-agent использует versioned SQLite schema, WAL и `synchronous=FULL`.
Checkpoint коллектора и новые outbox events фиксируются одной транзакцией.

## Этап 1. Control domain и reconciler — выполнен

Сделать control-plane владельцем нормализованных сущностей:

- `Node`, `Board`, `User`, `NodeAssignment`, `ConfigRevision`, `AuditEvent`;
- явные enabled/disabled/revoked состояния;
- optimistic version для конкурентных административных операций;
- детерминированный compiler сущностей в core `config.toml`;
- immutable revision с SHA-256 и ссылкой на предыдущую ревизию;
- desired/applied drift и сохранение последнего ApplyResult;
- event-driven уведомление node stream о новой ревизии плюс периодический
  reconcile как страховка.

Широкий `Repository` разделён на узкие порты enrollment, catalog, desired
revisions, node status, audit, notification и traffic ingestion. CLI
`catalog seed|node|board|user|assignment|reconcile|history` предоставляет
временную административную границу до появления HTTP API.

Критерии готовности:

- unit-тесты доменных инвариантов и TOML compiler golden tests;
- тест конкурентного обновления одной сущности;
- application-тест `change -> revision -> node apply -> applied projection`;
- reconnect ноды всегда приводит desired/applied к одному состоянию.

Все критерии закрыты unit, golden, optimistic concurrency, reconnect и
application flow тестами. Event bus ускоряет доставку внутри процесса, а
периодический reconcile восстанавливает её после потерянного уведомления или
изменения через отдельный CLI-процесс.

## Этап 2. Production backend API и хранилище

- PostgreSQL adapter и версионированные SQL migrations;
- шифрование приватных ключей envelope encryption, ключ шифрования вне БД;
- versioned HTTP API `/api/v1` для frontend;
- OpenAPI как проверяемый контракт;
- authentication, RBAC и append-only audit log;
- node list/read models: online, last seen, core readiness, version и drift;
- SSE для UI-обновлений; node gRPC браузеру не раскрывается.

Критерии готовности: repository contract suite выполняется для in-memory и
PostgreSQL adapters, API имеет authorization/validation tests, миграции
проверяются как с чистой БД, так и с предыдущей версии.

## Этап 3. Статистика

- идемпотентный ingestion по `(node_id, batch_id)`;
- ClickHouse для временных рядов и retention policy;
- отдельные таблицы interface traffic и per-user payload;
- hourly/daily rollups без сложения двух типов трафика;
- запросы для графиков, экспорта и лимитов пользователей;
- backpressure и ограничение локального outbox на ноде.

Тесты включают duplicate delivery, out-of-order batches, counter reset, restart
core, restart network namespace и недоступность telemetry storage.

## Этап 4. Frontend

После стабилизации read models:

- dashboard и alerts;
- ноды и desired/applied drift;
- пользователи, доски и assignments;
- история config revisions, diff и rollback;
- раздельные traffic charts;
- enrollment, certificates, audit и RBAC-aware actions.

## Этап 5. Production hardening

- несколько backend replicas и shared event delivery;
- certificate revocation/rotation и emergency node disable;
- rate limits, request size limits и secret redaction;
- backup/restore drills;
- rolling upgrades node/core;
- E2E с реальной доской, TCP/UDP клиентом и проверкой обеих метрик;
- failure tests: crash между persist/ACK, потеря hub и повреждённый desired
  snapshot.
