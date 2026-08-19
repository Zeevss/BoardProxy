# ТЗ: переписывание control-plane

Статус: **редакция 4, выполнено**. Все этапы закрыты; ниже — фактическое состояние, а не план.

Стек: Kotlin 2.2 / Spring Boot 4 / PostgreSQL 18. Доступ к данным остаётся рукописным JDBC на
`NamedParameterJdbcTemplate` — переход на ORM отклонён. Меняются модель домена, схема БД, контракт
ноды и HTTP API. Криптография, PKI, outbox и ArchUnit сохраняются.

Объём: было 9 627 строк Kotlin в 140 файлах, стало **8 808 в 136**.

Прогноз в 5 000–5 500 не оправдался, и это ошибка оценки, а не недоделанная чистка.
Удалено действительно много (лиз, NodeSession, проекция runtime, каталог, контекст subscriber,
два репозитория доступа), но добавилось больше, чем закладывалось: полный CRUD с тремя
сервисами и тремя контроллерами (~900 строк) — которого раньше не было вовсе, — модуль агентов,
история со снимками, diff и откатом. Прогноз делался до требования «полный CRUD независимо
от фронта» и после него не пересчитывался.

Согласовано:

- данные не переносим, пишем новый `V1` с нуля;
- фронтенд не учитываем, он переписывается позже; API проектируем полным, а не «под текущую панель»;
- **полный CRUD на все сущности** (ноды, борды, пользователи, гранты, квоты, подписки), включая
  реальное удаление — независимо от того, что панель сегодня умеет.

---

## 1. Зачем

### 1.1 Единица владения выбрана неверно

`Catalog` — агрегат «одна нода со всеми её бордами и пользователями» — заставил всю систему принять
per-node форму, хотя домен флотовый.

| Симптом | Где | Причина |
|---|---|---|
| Один пользователь = N строк в БД, N зашифрованных копий одного ключа, N квот | `users` PK `(node_id, id)` | пользователь смоделирован как часть агрегата ноды |
| Добавление пользователя на 3 ноды = 3 полные замены каталога + 6 снимков + 3 ревизии | `CatalogService.replace` | нет единицы изменения мельче ноды |
| Панель складывает лимиты по нодам, чтобы показать один лимит | `UserDirectory.kt` | флотовый вид собирается склейкой per-node срезов |
| Телеметрия пишет `DISABLED` прямо в каталог и обратно не включает | `TrafficQuotas.enforce` | нет способа выразить «квота влияет на конфиг» иначе, чем мутацией |
| Полная перевалидация агрегата на каждую правку одного поля | `Catalog.init` | инварианты дублируют CHECK/UNIQUE/FK из V1 |
| Конфликт версий между операторами, правящими разных пользователей одной ноды | `CatalogResources.mutate` | optimistic lock по версии ноды, а не сущности |
| `subscription_keys` — вторая копия размещений, способная разойтись с первой | `Subscription.keys` | подписке пришлось перечислять `(node, user)`, потому что пользователь не один |

### 1.2 Производное хранится как владеемое

`catalog_snapshots` (исходное состояние) и `desired_config_revisions` (скомпилированный TOML) — одно
и то же, зашифрованное дважды. Историческая скомпилированная форма не читается никогда: rollback
читает снимок и пишет **новую** ревизию.

### 1.3 Хаб помнит состояние сессии ноды, хотя нода знает его лучше

Отсюда лиз с fencing-токеном, `NodeSession` с `lastSent`/`retryAfter`, `NodeStatusExpiryJob` и вся
runtime-проекция с детекцией разрывов и rebuild-эндпоинтом.

При этом **правильный протокол в проекте уже есть** — канал сервиса подписок (§6.2). Задача не
изобрести его, а распространить на ноду.

---

## 2. Что не трогаем

- envelope-шифрование AES-256-GCM с keyring и активным `key_id`;
- файловый CA на BouncyCastle, enroll как единственный RPC без клиентского сертификата,
  локальная генерация ключа ноды, mTLS дальше;
- transactional outbox + `LISTEN/NOTIFY`;
- keylink и ссылка подписки никогда не хранятся собранными — всегда выводятся;
- `CoreConfigRedaction` перед отдачей конфига в панель;
- резервный канал подписок через Яндекс Таблицу (Noise IK, `recoveryPublicKey`) — рабочий и
  не завязан на модель;
- разделение interface-трафика и per-user payload как двух несмешиваемых деревьев, почасовые
  роллапы, retention;
- SSE как транспорт панели (правится только реализация фан-аута);
- ArchUnit как способ удерживать границы (плюс одно новое правило, §10);
- Testcontainers как основной вид интеграционного теста.

---

## 3. Целевая доменная модель

### 3.1 Сущности

```kotlin
// provisioning/domain/model

/** Нода — процесс ядра, обслуживающий свои борды. */
data class Node(
    val id: String, val name: String, val state: ResourceState,
    val settings: CoreSettings, val serverKey: ServerKey,
    val version: Long, val updatedAt: Instant,
)

/** Борд принадлежит ровно одной ноде: hub-slide не делится между серверами. */
data class Board(
    val nodeId: String, val id: String, val name: String, val hash: String,
    val hubSlide: String?, val apiBase: String?, val guestName: String?,
    val state: ResourceState, val maxLanes: Int,
    val version: Long, val updatedAt: Instant,
)

/** Пользователь — флотовая сущность. Один человек — одна строка, один ключ, одна квота. */
data class User(
    val id: String, val name: String, val identity: UserIdentity,
    val state: ResourceState, val maxSessions: Int, val maxLanes: Int,
    val version: Long, val updatedAt: Instant,
)

/** Размещение: на какой ноде и на каких её бордах пользователь имеет доступ. */
data class Grant(val userId: String, val nodeId: String, val boardIds: Set<String>)
```

Следствия: `identity_fingerprint` уникален глобально; приватный ключ хранится один раз (сегодня
`UserProvisioning` переиспользует один X25519-ключ на все ноды и шифрует его N раз); квота одна и
считается по сумме трафика со всех нод.

### 3.2 Чего больше нет

- `Catalog` как агрегат. Остаётся `NodeConfigInput` — read-model компилятора, собираемая одним
  запросом, без `init`-валидации и без версии.
- `NodeAssignment` / `assignment_versions` — размещение это строки `grants`.
- Bounded context `subscriber` целиком: `FleetUser`, `UserPlacement`, `UserDirectory`.
  `User` **и есть** флотовый пользователь; `/api/v1/users` переезжает в `provisioning`.
- `SubscriptionKey` и таблица `subscription_keys` (§6.1).

### 3.3 Где живут инварианты

| Инвариант | Где |
|---|---|
| уникальность `board_hash` в пределах ноды | `UNIQUE (node_id, board_hash)` |
| глобальная уникальность отпечатка пользователя | `UNIQUE (identity_fingerprint)` |
| грант ссылается на борд той же ноды | FK `(node_id, board_id) → boards(node_id, id)` |
| ровно одно из «приватный ключ» / «публичный ключ» | CHECK |
| `max_lanes` 1..32, `max_sessions >= 0` | CHECK |
| корректность TOML | тесты компилятора |

В домене остаются только правила, которые БД выразить не может: формат `board_hash`, нормализация
`api_base`, совместимость `max_lanes` борда и пользователя. Остальные `init`-блоки удаляются.

---

## 4. Компиляция конфига

### 4.1 Чистая функция

```kotlin
fun interface CoreConfigCompiler { fun compile(input: NodeConfigInput): ByteArray }

data class NodeConfigInput(val node: Node, val boards: List<Board>, val users: List<UserOnNode>)
data class UserOnNode(val user: User, val boardIds: Set<String>, val quotaExceeded: Boolean)
```

Эффективное состояние: `user.state == ENABLED && !quotaExceeded`.
Компилятор остаётся детерминированным; `Duration.goString()` и TOML-экранирование переносятся
без изменений (см. риск R3).

### 4.2 Публикация

Единственная точка записи desired state:

```kotlin
class DesiredConfigPublisher {
    /** Пересобирает конфиг затронутых нод; ревизия появляется только при изменении байт. */
    fun publish(nodeIds: Set<String>, cause: String, actor: String): List<PublishResult>
}
```

```
для каждой nodeId:
    toml = compiler.compile(loader.load(nodeId))
    sha  = sha256(toml)
    если текущая sha совпала -> Unchanged, ничего не пишем
    иначе: upsert node_desired_config(revision + 1, sha, encrypt(toml))
           save node_config_snapshots(исходное состояние, cause, actor)
           outbox("desired-state.changed", {nodeId, revision, sha})
```

Множество затронутых нод передаётся **явно** вызывающим сервисом внутри той же транзакции — никакого
фонового dirty-tracking. Для правки пользователя это `grants.nodesOf(userId)`, для борда — `setOf(board.nodeId)`.

Что чинится: правка пользователя на 3 нодах = 3 конфига вместо 3 полных замен агрегата;
`configChanged` становится сравнением байт вместо эвристики
`revision.catalogVersion == catalog.version` (`CatalogService.kt:107`, косвенный способ узнать,
сработал ли `ON CONFLICT` по существующему `UNIQUE (node_id, config_sha256)`).

### 4.3 История

Хранится только исходное состояние: `node_config_snapshots` — одна строка на изменение (сегодня
`replace` пишет две: текущую и новую). `desired_config_revisions` заменяется на `node_desired_config`
— одна текущая строка на ноду. Diff считается по снимкам. Rollback = снимок → обычные записи →
`publish`, то есть ровно то, что происходит сегодня.

---

## 5. Управляемые агенты и протокол ноды

### 5.1 Общее понятие

Нода и `subscribe` — два экземпляра одного понятия: внешний процесс с desired-конфигом, монотонной
ревизией, отчётом о наблюдаемом состоянии и одноразовыми командами.

Общими делаются **таблицы и семантика**, но не транспорт:

- общее: `agent_status` (last_seen, applied_revision, версия, ошибка), `agent_commands`
  (одноразовая доставка с ack на стороне хаба), понятие «онлайн = отчитывался не позже 45 с назад»,
  один экран «Агенты» в панели;
- раздельное: транспорт (у ноды mTLS/gRPC, у `subscribe` bearer/HTTP) и публикация конфига
  (у ноды компилятор TOML, у `subscribe` запись настроек).

Абстрагировать транспорт не нужно — выгоды нет, а индирекция есть.

### 5.2 Новый контракт ноды

`control-plane/contracts/node/v1/node.proto`:

```proto
service NodeControlService {
  rpc Enroll(EnrollRequest) returns (EnrollResponse);
  rpc Renew(RenewRequest)   returns (EnrollResponse);

  // Хаб шлёт только номер ревизии. Полезной нагрузки в потоке нет.
  rpc Watch(WatchRequest) returns (stream ConfigNotice);

  // Нода тянет конфиг сама, когда номер разошёлся с применённым.
  rpc FetchConfig(FetchConfigRequest) returns (ConfigDocument);

  // Всё, что нода сообщает хабу. Unary, идемпотентно по batch_id.
  rpc Report(ReportRequest) returns (ReportResponse);
}

message ConfigNotice   { uint64 revision = 1; string config_sha256 = 2; }
message ConfigDocument { uint64 revision = 1; string config_sha256 = 2; bytes config_toml = 3; }

message ReportRequest {
  string node_id  = 1;
  string boot_id  = 2;   // меняется при рестарте ядра
  uint64 seq      = 3;   // монотонный в пределах boot_id
  string batch_id = 4;   // ключ идемпотентности

  Health health = 5;                                   // обязателен в каждом Report
  RuntimeSnapshot runtime = 6;                         // полный снимок, опционален
  repeated InterfaceTrafficDelta interface_traffic = 7;
  repeated UserTrafficDelta user_traffic = 8;
  repeated RuntimeEvent events = 9;                    // журнал, без обязанностей проекции
}

message Health {
  uint64 applied_revision = 1;
  string applied_sha256   = 2;
  string apply_error      = 3;   // пусто = применено успешно
  string core_version     = 4;
  int64  uptime_seconds   = 5;
}

message ReportResponse { repeated AgentCommand commands = 1; }   // напр. restart, ровно один раз
```

### 5.3 Поведение ноды

1. Держит `Watch`; на каждый `ConfigNotice` сравнивает `revision` со своей применённой.
2. Разошлось — зовёт `FetchConfig`, применяет, результат кладёт в `Health` следующего `Report`.
   **Ретраи целиком на стороне ноды**, `FetchConfig` идемпотентен.
3. Раз в 30 с и при обрыве `Watch` — `FetchConfig` без ожидания уведомления. Это reconcile, и он
   тривиально корректен, потому что хабу для него не нужно ничего помнить.
4. Раз в N секунд — `Report` с `Health` и накопленной телеметрией.

### 5.4 Что исчезает на стороне хаба

| Убирается | Чем заменено |
|---|---|
| `node_session_leases`, fencing-токены, acquire/renew/release | ничем: `Report` не несёт кэшированного состояния, поздний запрос не может перезаписать свежий |
| `NodeSession` с `applied`/`lastSent`/`retryAfter`/`RETRY_DELAY` | `Health.applied_revision` в каждом `Report` |
| `NodeStatusExpiryJob` (UPDATE каждые 15 с) | `now() - last_report_at < 45s`, считается при чтении |
| `channelFlow` + Hello-first машина состояний | `Watch` — server-streaming, обработчик без состояния |
| `@Synchronized` вокруг блокирующего JDBC из корутин | блокирующий обработчик на виртуальном потоке (§8.2) |
| два разных механизма дедупликации батчей | одна таблица `agent_reports(agent_id, batch_id)` |

«Один агент на ноду» перестаёт быть жёстким инвариантом и становится наблюдаемым фактом: мелькающий
`boot_id` — предупреждение в панели. Это честнее: лиз всё равно не мешал второму агенту писать
телеметрию.

### 5.5 Runtime: снимок вместо event sourcing

Нода шлёт полный снимок runtime-состояния (пользователи × сессии, борды × состояние) периодически и
при изменении. Хаб хранит последний в `node_runtime` (jsonb, замена целиком).

Убирается: `RuntimeProjection.apply/replace`, детекция разрывов, `gap_detected`,
`sessionDetailsComplete`, `EventStreamReset`, `runtime_event_batches.snapshot`,
`RuntimeProjectionRebuildService`, `POST /runtime/rebuild`, уникальный индекс по
`(node_id, core_boot_id, sequence)`.

`runtime_events` остаётся append-only журналом активности: `(node_id, received_at, type, payload)` +
retention. Цена: теряется точная история отдельных сессий — она и сегодня негарантированная,
флаг `sessionDetailsComplete` существует ровно поэтому.

---

## 6. Подписки

В контексте `subscription` сегодня живут две несвязанные системы. Разводим их.

### 6.1 Подписка

**Находка:** `subscription_keys(node_id, user_id, position)` — денормализованная копия размещений.
README `subscribe` подтверждает: «Одна подписка содержит по одному ключу для каждой целевой ноды».
Две копии уже могут разойтись: добавление ноды пользователю через `PUT /nodes/{id}/users/{userId}`
не создаёт ключ в подписке.

С флотовым пользователем подписка становится указателем на него:

```kotlin
data class Subscription(
    val id: String, val name: String,
    val userId: String,               // одна подписка — один пользователь
    val tokenHash: String,            // секрет хранится ещё и зашифрованным — ссылка восстановима
    val recoveryPublicKey: String,
    val state: SubscriptionState,
    val version: Long, val createdAt: Instant, val updatedAt: Instant,
)
```

`resolve(token | recoveryPublicKey)` разворачивает подписку через гранты: на каждый грант — keylink,
состояние и трафик по этой ноде. Пользователь может иметь несколько подписок (ротация, разные
устройства).

Удаляются: `subscription_keys`, класс `SubscriptionKey` с пятью инвариантами, поле `position`,
`validateTargets` (сегодня — `catalogs.get` на каждую ноду при каждой записи).

Два дефекта, которые чинятся тем же движением:

- `SubscriptionService.resolve:158` зовёт `traffic.userTotals(nodeId, Instant.EPOCH, now)` —
  **вытягивает тоталы всех пользователей ноды от начала времён, чтобы прочитать одного**, на каждый
  запрос страницы подписки. Становится индексированным чтением `user_traffic_quota_state`.
- `SubscriptionSnapshot.trafficLimit` имеет дефолт `0` и нигде не выставляется — на странице
  подписки лимит всегда ноль. С флотовой квотой становится настоящим числом.

Ротация, восстановимая ссылка, capsule во фрагменте `#bp1=` и резервный канал через Яндекс Таблицу
переносятся без изменений.

### 6.2 Сервис подписок

Канал `POST /api/v1/subscription-service/poll` — образец для §5:

| `subscribe` (существует) | нода (предлагается) |
|---|---|
| `POST /poll {revision, serviceVersion, …}` | `Report {seq, Health{applied_revision}}` |
| `204 No Content` = у тебя актуально | `ConfigNotice` не расходится с applied |
| «подключен» = приходил не позже 45 с назад | `now() - last_report_at < 45s` |
| отдельного heartbeat нет — запрос им и является | то же |
| нет лиза, нет fencing | то же |
| `restartNonce`/`ackedRestartNonce`, доставка ровно один раз, факт ведёт хаб | `AgentCommand` в `ReportResponse` |

Меняется только подстилающее хранилище: `subscription_service_status` → общий `agent_status`,
`restart_nonce`/`acked_restart_nonce` → общий `agent_commands`. Контракт HTTP остаётся прежним,
Go-сервис править не нужно.

### 6.3 Полная картина взаимодействия

```
Оператор → POST /api/v1/users {name, nodes[], quota}
   хаб: User + Grants + Quota + Subscription в одной транзакции
        → publish(nodes) → ревизии → ноды применяют
        ← https://sub.example/s/<token>#bp1=<capsule>

Обычный путь:
   Клиент → GET /s/<token> → subscribe
   subscribe → POST /api/v1/subscriptions/resolve {token} → хаб
             ← snapshot: keys[keylink, usedBytes, state], usedBytes, trafficLimit
             → HTML или JSON по Accept

Резервный путь (только при сетевой ошибке или 5xx; 4xx окончателен):
   SDK → Яндекс Таблица: BP1 hello + Noise IK
   subscribe → POST /api/v1/subscriptions/resolve {recoveryPublicKey} → хаб
             → зашифрованный ответ в комментарий

Конфигурация subscribe:
   subscribe → POST /api/v1/subscription-service/poll {revision, …} каждые 15 с
             ← 200 с конфигом (+ recoveryPrivateKey, + restartRequested) либо 204
```

### 6.4 Дедупликация криптографии

Вывод публичного X25519 по базовой точке 9 написан в Kotlin **трижды**: `Keylink.kt`,
`RecoveryKeys.kt` и приватная `x25519Public()` в конце `SubscriptionService.kt`. `base64Url()` и
`sha256Hex()` продублированы в нескольких файлах. Всё сводится в `shared/crypto/X25519.kt` и
`shared/crypto/Encoding.kt`.

---

## 7. Квоты как вход компиляции

Телеметрия перестаёт быть писателем desired state; `TrafficQuotaService` больше не импортирует
`CatalogResourceCommands`.

```
telemetry: считает used_bytes за период → пишет user_traffic_quota_state.exceeded
           → outbox("quota.changed", {userId})
           → подписчик зовёт DesiredConfigPublisher.publish(grants.nodesOf(userId))
```

Свойства: поведение симметрично — новый период → `exceeded = false` → конфиг пересобрался →
пользователь включён (сегодня оператор включает руками). Квота флотовая, одна строка на
пользователя; складывание лимитов в `UserDirectory` исчезает. `ALERT` пишет только событие,
`RESET` обнуляет счётчик, `DISABLE` влияет на конфиг.

---

## 8. Инфраструктура

### 8.1 Доступ к данным: остаётся рукописный JDBC

Переход на ORM (рассматривались jOOQ, Exposed, JPA) **отклонён**. Слой доступа к данным остаётся
таким, какой есть: `NamedParameterJdbcTemplate`, ручной SQL, ручные row-мапперы, репозитории только
в `infrastructure`, порты в `application`, `TransactionRunner` без изменений.

Следствие, которое надо компенсировать: рукописный SQL не ломается на переименованной колонке —
расхождение схемы и кода видно только в рантайме. Компенсация: `MigrationContractTest` (уже есть)
расширяется до проверки, что каждая колонка, читаемая репозиториями, существует в схеме, и
Testcontainers-тест на каждый репозиторий обязателен, а не желателен.

Правила, чтобы 5 000 строк SQL не превратились в то же, что сейчас:

- один репозиторий — одна таблица (или один явно названный кластер таблиц), без «сервисных» запросов
  через границу контекста;
- вся Postgres-специфика (`ON CONFLICT … WHERE … RETURNING`, частичные индексы, оконные функции
  роллапов) — плюс, а не проблема: именно ради неё ORM и отклонён;
- SQL-константы держим рядом с местом использования, а не в общем `Queries`-объекте.

### 8.2 Виртуальные потоки вместо корутин

`spring.threads.virtual.enabled=true`. gRPC-обработчики переписываются на блокирующий
`NodeControlServiceImplBase` из grpc-java. Из сборки уходят `grpc-kotlin-stub`,
`protoc-gen-grpc-kotlin` и `kotlinx-coroutines-core`; из кода — `channelFlow`, координация двух
направлений потока и класс проблем «блокирующий JDBC в корутине».

### 8.3 Прочее

- **Flyway** остаётся, один `V1` с нуля.
- **outbox + LISTEN/NOTIFY** остаются даже на одной реплике: таблица даёт долговечность, экран
  dead-letters уже есть.
- **SSE**: один общий подписчик `LocalControlEventBus` и общая очередь вместо
  `ThreadPoolExecutor(1,1,…)` на каждое соединение.
- **Micrometer/Prometheus** уже в зависимостях — добавить метрики: publish/skip по нодам, приём
  отчётов, staleness агентов, отказы resolve.
- **Testcontainers**: один синглтон-контейнер на прогон вместо контейнера на класс.
- **Rate limiting**: остаётся в памяти процесса. Хаб однорепличный (O4), так что это корректно;
  ограничение `MAX_KEYS = 100_000` сохраняется.

---

## 9. Схема БД

Новый `V1`, без переноса данных.

```
nodes(id PK, name, state, core_settings jsonb, server_key_*, version, updated_at)

boards(node_id, id, name, board_hash, hub_slide, api_base, guest_name, state, max_lanes,
       version, updated_at, PK (node_id, id), UNIQUE (node_id, board_hash))

users(id PK, name, private_key_*, public_key, identity_fingerprint UNIQUE,
      state, max_sessions, max_lanes, version, updated_at)

grants(user_id → users, node_id, board_id, PK (user_id, node_id, board_id),
       FK (node_id, board_id) → boards(node_id, id) ON DELETE CASCADE)

node_desired_config(node_id PK, revision, config_sha256, config_*, updated_at)
node_config_snapshots(node_id, seq, state jsonb зашифрован, cause, actor, created_at,
                      PK (node_id, seq))
node_runtime(node_id PK, snapshot jsonb, observed_at)
runtime_events(id, node_id, received_at, type, payload jsonb)

-- общее для ноды и subscribe
agents(id PK, kind, name, state)
agent_status(agent_id PK, boot_id, seq, applied_revision, version, error, last_report_at, details jsonb)
agent_commands(agent_id, nonce, kind, issued_at, delivered_at, PK (agent_id, nonce))
agent_reports(agent_id, batch_id, received_at, PK (agent_id, batch_id))

interface_traffic_deltas(...)
user_traffic_deltas(node_id, user_id, ...)
traffic_hourly_rollups(...)
user_traffic_quotas(user_id PK, limit_bytes, period, action, updated_at)
user_traffic_quota_state(user_id PK, period_start, used_bytes, exceeded, changed_at)

subscriptions(id PK, name, user_id → users, token_hash, token_ciphertext_*,
              recovery_public_key UNIQUE, recovery_private_*, state, version,
              created_at, updated_at)
subscription_service_settings(...)      как сейчас, минус status/nonce (ушли в agent_*)

credentials(id PK, kind, subject, secret_hash, role, created_at, expires_at,
            last_used_at, revoked_at)
panel_administrators(...)
enrollment_tokens, node_certificates, audit_events, outbox_events   как сейчас
```

Не переносятся: `node_boards`, `node_users` (избыточны — PK и FK совпадают с PK самих
`boards`/`users`), `node_user_boards` и `assignment_versions` (→ `grants`),
`desired_config_revisions` (→ `node_desired_config`), `node_session_leases`,
`node_runtime_projection`, `runtime_event_batches`, `traffic_batches` (→ `agent_reports`),
`subscription_keys`, `subscription_service_status`, `api_tokens` и `panel_sessions`
(→ `credentials`: сегодня это ~200 строк почти дублирующей логики над двумя одинаковыми таблицами).

---

## 10. HTTP API

Полный CRUD на все сущности, включая удаление. Пагинация везде, где возможен рост.
Optimistic lock — заголовком `If-Match: <version>` **на конкретной сущности**, а не на версии ноды:
два оператора, правящие разных пользователей одной ноды, больше не конфликтуют.

```
POST   /api/v1/auth/setup | /login | /logout
GET    /api/v1/auth/status | /me

GET    /api/v1/nodes                                 пагинация, фильтры
POST   /api/v1/nodes
GET|PATCH|PUT|DELETE /api/v1/nodes/{id}
GET    /api/v1/nodes/{id}/config                     текущий TOML, редактированный
GET    /api/v1/nodes/{id}/history
GET    /api/v1/nodes/{id}/history/diff
POST   /api/v1/nodes/{id}/history/{seq}/rollback
GET    /api/v1/nodes/{id}/runtime
POST   /api/v1/nodes/{id}/enrollment-tokens
GET    /api/v1/nodes/{id}/certificates
DELETE /api/v1/nodes/{id}/certificates/{serial}

GET    /api/v1/boards?nodeId=&query=                 пагинация
POST   /api/v1/boards
GET|PATCH|PUT|DELETE /api/v1/boards/{id}

GET    /api/v1/users?query=&nodeId=&state=           пагинация
POST   /api/v1/users
GET|PATCH|PUT|DELETE /api/v1/users/{id}
GET|PUT /api/v1/users/{id}/grants
PUT|DELETE /api/v1/users/{id}/quota
POST   /api/v1/users/{id}/key/rotate
GET    /api/v1/users/{id}/keylinks

GET    /api/v1/subscriptions?userId=                 пагинация
POST   /api/v1/subscriptions
GET|PATCH|PUT|DELETE /api/v1/subscriptions/{id}
POST   /api/v1/subscriptions/{id}/rotate
GET    /api/v1/subscriptions/{id}/link
POST   /api/v1/subscriptions/resolve                 для subscribe

GET|PUT /api/v1/subscription-service
POST   /api/v1/subscription-service/token | /restart
POST   /api/v1/subscription-service/poll             для subscribe

GET    /api/v1/agents                                ноды + subscribe одним списком
GET    /api/v1/traffic?nodeId=&userId=&from=&to=
GET    /api/v1/traffic/series
GET    /api/v1/audit                                 пагинация
GET    /api/v1/events                                SSE
GET    /api/v1/operations/outbox/dead-letters
POST   /api/v1/operations/outbox/dead-letters/{id}/retry
```

`DELETE` семантика: у ноды — каскад по бордам, грантам, конфигу, статусу и телеметрии, с явным
подтверждением; у пользователя — каскад по грантам, квоте и подпискам; у борда — отзыв грантов на
него. `REVOKED` остаётся как мягкое состояние для «отключить, но сохранить историю».

---

## 11. Тесты и правила

- **Новое ArchUnit-правило**: application-слой контекста не зависит от application-слоя другого
  контекста, кроме явно перечисленного списка пар. Сегодня такого правила нет — именно поэтому
  связки §1 возникли незамеченными.
- Компилятор: golden-тесты TOML, включая `quotaExceeded`, `DISABLED`, `REVOKED`, пустой набор бордов.
- `DesiredConfigPublisher`: правка, не меняющая TOML, не создаёт ревизию; правка пользователя на
  3 нодах создаёт ровно 3 ревизии.
- `Report`: повтор с тем же `batch_id` не удваивает трафик; более старый `seq` не откатывает статус.
- Подписка: `resolve` после добавления гранта немедленно отдаёт новый keylink (сегодня требуется
  отдельная правка подписки).
- Совместимость с ядром: тест, прогоняющий скомпилированный TOML через реальный парсер Go (R3).

---

## 12. Этапы

Каждый этап — отдельный коммит с зелёными тестами.

| № | Этап | Критерий готовности |
|---|---|---|
| 0 | Зафиксировать базу (O2), включить виртуальные потоки, убрать корутины из gRPC | `./gradlew build` зелёный |
| 1 | Новый `V1` + контракт схемы на живой базе | `MigrationContractTest` зелёный на Testcontainers |
| 2 | Домен + `CoreConfigCompiler` как чистая функция + golden-тесты | TOML побайтово совпадает с текущим для эквивалентных входов |
| 2b | Репозитории на JDBC под новую схему | Testcontainers-тест на каждый репозиторий |
| 3 | `DesiredConfigPublisher`, снимки, история, rollback | `Catalog`, `CatalogResources`, `subscriber` удалены |
| 4 | Полный CRUD + пагинация + `If-Match` на сущностях | контрактные тесты на все эндпоинты §10 |
| 5 | Квоты как вход компиляции | `telemetry` не зависит от `provisioning.application`; ArchUnit-правило включено |
| 6 | Подписки: `userId` вместо `subscription_keys` | `resolve` без `catalogs.get` в цикле; `trafficLimit` не ноль |
| 7 | Агенты: общие `agent_*`, канал `subscribe` переезжает без смены HTTP-контракта | Go-сервис работает без правок |
| 8 | Новый `node.proto`, `Watch`/`FetchConfig`/`Report`, блокирующие обработчики | интеграционный тест с фейковой нодой |
| 9 | Runtime-снимок вместо проекции, слияние `credentials`, чистка | `runtime` < 250 строк |
| 10 | Обновление Go node-agent (O3) | e2e: enroll → watch → apply → report |

Этапы 1–7 не меняют контракт с нодой и с `subscribe` — их можно вести до готовности этапа 10.

**Репозитории идут после домена, а не вместе со схемой.** Изначально этапы 1 и 2 стояли наоборот,
но репозитории возвращают доменные объекты, поэтому домен обязан существовать раньше. Отсюда 2b.

**Продукт нерабочий между этапами 1 и 9.** Схема заменена целиком, поэтому весь старый слой доступа
к данным обращается к несуществующим таблицам. Критерий «зелёный этап» означает «компилируется и
тесты проходят», а не «система обслуживает трафик». Это цена переписывания, а не регрессия.

---

## 13. Риски

- **R1. Даунтайм при смене протокола ноды.** Принято синхронное обновление (O3): хаб и все ноды
  выкатываются вместе, `Connect` удаляется. Между выкаткой хаба и обновлением ноды нода не
  обслуживается. Митигация — этап 10 идёт одним релизом, а не двумя.
- **R2. Потеря истории.** Скомпилированные TOML прошлых ревизий не сохраняются (и сегодня не
  читаются). Если история нужна юридически — сказать сейчас.
- ~~**R3. Kotlin переизобретает Go.**~~ **Закрыт.** Общая фикстура
  `contracts/testdata/hub-config.toml`: Kotlin-тест сверяет с ней байты компилятора,
  Go-тест `core/internal/serverconfig/hubconfig_test.go` парсит её настоящим `Decode`.
  Расхождение роняет один из двух тестов. Исходная формулировка риска: `goString()` (ручная реализация `time.Duration.String()`), схема
  TOML (дублирует `core/internal/serverconfig`), деривация keylink через X25519 (дублирует `core`).
  Три реализации на критическом пути, расхождение видно только в проде. Раз остаёмся на Kotlin —
  **обязателен** CI-тест, гоняющий скомпилированный TOML через реальный парсер ядра.
- **R4. Отказ от лиза.** Два агента с одним `node_id` больше не отсекаются на входе. Митигация —
  мелькающий `boot_id` как предупреждение в панели.
- **R5. Рукописный SQL и новая схема.** Переход на ORM отклонён, значит расхождение схемы и кода
  ловится только тестами. Схема меняется целиком, то есть переписывается каждый запрос в каждом
  репозитории. Митигация — §8.1: расширенный `MigrationContractTest` и обязательный
  Testcontainers-тест на каждый репозиторий, без исключений.

---

## 14. Решённые вопросы

- **O1. Перенос данных** — не переносим, пишем `V1` с нуля.
- **O2. Рабочее дерево** — фиксируется коммитом автора перед этапом 1; фронтенд в объём не входит.
- **O3. Go node-agent** — обновляется синхронно, период совместимости не нужен. `Connect` из
  `node.proto` удаляется, а не депрецируется. Этап 10 обязателен и завершает переписывание.
- **O4. Многорепличность** — одна реплика. Rate limiting остаётся в памяти процесса,
  outbox + `LISTEN/NOTIFY` сохраняются ради долговечности и экрана dead-letters, а не ради
  горизонтального масштабирования.
