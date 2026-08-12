# BoardProxy: краткая спецификация проекта

## 1. Назначение и состав

BoardProxy — SOCKS5 TCP/UDP и HTTP-прокси, у которого транспортом служат текстовые объекты
на страницах Yandex Board. Создание объекта передаёт пакет, удаление объекта
подтверждает его получение. Репозиторий состоит из следующих частей:

- `core/` — stateless Go-бинарник `bproxy`: data plane и локальный management API;
- `node-agent/` — агент ноды: запускает core, применяет desired config,
  собирает два независимых потока трафика и гарантированно доставляет их хабу;
- `control-plane/` — Kotlin/Spring Boot modular monolith: нормализованный control
  catalog, compiler, immutable revisions, reconciler, enrollment, mTLS node
  stream и PostgreSQL-хранилище;
- `docker-compose.yml` — локальная/dev-связка PostgreSQL + control-plane + node;
  отдельного panel/gateway и старого Go control-plane больше нет.

Главный путь данных:

```text
локальное приложение
  -> SOCKS5/HTTP proxy
  -> mux stream или datagram association
  -> bond PacketID
  -> один из двух link lane
  -> объект на отдельной странице доски
  -> link -> bond -> mux
  -> server egress (TCP dial или UDP socket)
  -> целевой сервер
```

## 2. Core: слои и зависимости

Точка входа — `core/cmd/bproxy/main.go`. `serve` загружает строгий versioned
TOML, а `users`, `boards`, `reload`, `stats` обращаются к gRPC уже запущенного
процесса. Композицию и reconciliation выполняет `internal/app`.

| Модуль | Роль | Что смотреть при отладке |
|---|---|---|
| `internal/board` | Интерфейс транспорта `Session` | Контракт `Subscribe/Put/Delete/Events/Reconnects` |
| `internal/board/yandex` | REST + Socket.IO драйвер доски | Join, подписка, события, повторный dial и snapshot после reconnect |
| `internal/board/memory` | In-process реализация | Детерминированные тесты без Yandex и сети |
| `internal/codec`, `crypto` | Кодирование и шифрование объектов | Маркер протокола, Z85/base64, XChaCha20-Poly1305 |
| `internal/handshake`, `keylink` | Noise IK и credentials | Проверка ключей, `bproxy://...`, получение traffic keys |
| `internal/link` | Надёжный канал одной страницы | seq, ACK-by-delete, reassembly, окна, heartbeat, reconcile, Flush |
| `internal/bond` | Логическое соединение из нескольких страниц | PacketID, round-robin, dedup, bounded replay, немедленная unordered-доставка и перенос unacked при потере lane |
| `internal/mux` | TCP-стримы и UDP associations | SYN, DATA(offset), FIN(final offset), RESET, абсолютный MAX_STREAM_DATA, per-stream reorder и DATAGRAM |
| `internal/hub` | Rendezvous и пул страниц | NEW_BUNDLE/JOIN_LANE, авторизация через `UserDirectory`, независимый acquire/release lane, live bundles |
| `internal/proxy` | Клиентский вход | SOCKS5 CONNECT/UDP ASSOCIATE, HTTP, local UDP relay socket |
| `internal/egress` | Серверный выход | TCP relay и UDP socket на каждую association |
| `internal/serverconfig` | Desired state | Строгий TOML v1: ключи, пользователи, доски, лимиты, listeners |
| `internal/control` | Policy/runtime state | Atomic snapshot пользователей, глобальные session limits, эфемерные счётчики |
| `internal/controlapi` | Управляющий gRPC API | optimistic revision, reload и hot add/update/remove ресурсов |
| `internal/telemetry` | Наблюдаемость | Secret-free статистика только с момента старта |
| `internal/mgmt` | Read-only HTTP adapter | Только health/stats/recent in-memory logs |
| `pkg/bproxy` | Встраиваемый клиент и его композиция | Board sessions → link lanes → bond→mux, статусы, метрики, reconnect-loop, system proxy |
| `pkg/mobile` | Android binding facade | JSON config, callbacks, async lifecycle, VpnService protector |
| `internal/netprotect` | VPN-safe dialer | Вызов Android `VpnService.protect(fd)` перед connect |
| `internal/app` | Композиционный корень | Создание конкретных board/link/mux/hub/proxy объектов |

Зависимости направлены сверху вниз через небольшие интерфейсы. В частности,
`mux` не импортирует `link`, а `link` работает с `board.Session`. Поэтому сбой
лучше локализовать по границе слоя, а затем воспроизвести на `board/memory`.

## 3. Подключение и жизненный цикл

1. Клиент присоединяется к доске и подписывается на hub-слайд.
2. Клиент создаёт `HELLO`: Noise IK message 1 и случайный correlation nonce.
3. Сервер проверяет криптографическую личность по atomic policy snapshot, создаёт bundle и
   занимает первую страницу в `pagePool`.
4. Сервер создаёт отдельную board-сессию, подписывается на страницу и отвечает
   `ASSIGN`; id страницы находится внутри зашифрованного Noise message 2.
5. Обе стороны создают `crypto.Sealed -> link.Link -> bond.Conn -> mux.Session`
   и делают `Reconcile` со snapshot страницы.
6. Клиент открывает независимую board-сессию и делает `JOIN_LANE` с BundleID,
   Epoch и секретным join token. Второй link добавляется в тот же bond; новый
   mux/egress не создаётся. Если страницу выделить нельзя, остаётся первый lane.
7. Сервер регистрирует один bundle в `clients`/`byUser`, передаёт его mux в
   `egress` и запускает отдельные watcher'ы bundle и каждого lane.
8. При штатном закрытии `mux.Close` отправляет GOAWAY, вызывает `bond.Flush`,
   затем закрывает оба link и обе board-сессии. Пир немедленно закрывает свой mux.
9. `watchLane` закрывает lane, очищает страницу отдельной board-сессией и только
   после двух пустых snapshot возвращает её в пул. Перед новым `ASSIGN`
   выполняется такая же очистка; ошибка оставляет страницу в карантине. Потеря одного lane не
   закрывает mux; неподтверждённые PacketID переигрываются через оставшийся.
   `watchBundle` один раз добавляет итоговый трафик в process-local счётчики. При аварийном исчезновении
   тот же путь запускает `IdleTimeout`: живой
   клиент подтверждает присутствие link-heartbeat'ами, исчезнувший — нет.
   Сервер учитывает только валидные события удалённого участника, а не эхо своих
   объектов. Штатный timeout в compose — 90 секунд (три heartbeat-интервала).
10. Клиентский `pkg/bproxy.Client` замечает `mux.Done`, сообщает
   `reconnecting` и повторяет rendezvous с экспоненциальным backoff. Локальный
   listener остаётся на том же порту и начинает направлять новые соединения в
   свежую mux-сессию. Локальный `Stop` сначала сообщает `stopping`, затем после
   очистки — `disconnected`.

WebSocket-сбой внутри живой board-сессии обрабатывается ниже: драйвер Yandex
повторяет dial и subscribe, публикует свежий snapshot через `Reconnects()`, а
`link` освобождает уже подтверждённые слоты и переигрывает пропущенные объекты.
Отсутствие Engine.IO ping дольше объявленных сервером `pingInterval +
pingTimeout` считается обрывом даже без TCP EOF — это покрывает suspend и смену
сетевого интерфейса.
Это сохраняет mux-стримы. GOAWAY, окончательный провал board reconnect или
рестарт процесса уже создают новую hub/mux-сессию.

Текущий wire protocol v5 использует unordered bond и per-stream offsets, а в
зашифрованном rendezvous assignment передаёт серверный `max_lanes` конкретной
доски. Формат v4 сохранён для negotiated fallback при поэтапном обновлении.

## 4. Core, node-agent, control-plane и статистика

TOML — единственный долговечный desired state core. gRPC применяет изменения к
живому runtime без рестарта: пользовательская policy публикуется атомарно,
затронутые сессии закрываются, доски независимо добавляются, отключаются или
заменяются. Доска с временно недоступным API переходит в `retrying` и сама
восстанавливается с ограниченным exponential backoff. `ApplySnapshot` атомарно
заменяет весь срез пользователей и досок. Каждая mutation проверяет
`expected_revision`. Изменения gRPC не
записываются обратно: следующий `Reload` снова делает файл источником истины.
Отдельные `AddUser`/`AddBoard` возвращают `ALREADY_EXISTS` при конфликте тега,
тогда как `Replace*` предназначены для idempotent reconciliation. CLI после
reactive add напоминает перенести ресурс в долговечный source-конфиг.

Core не хранит историческую статистику. Он отдаёт active users/connections/
lanes/streams, payload bytes since start по каждому пользователю, состояние
pool/cleanup и transport reconnect counters. Интерфейсные Linux-счётчики из
core удалены: это ответственность node-agent.

Node-agent инициирует единственный исходящий bidirectional gRPC stream к хабу
по mTLS. Одноразовый bootstrap secret содержит node ID, адрес хаба, token и CA,
но не приватный ключ. Ключ ноды и CSR создаются локально; сертификат автоматически
обновляется до истечения. Desired TOML приходит с монотонной revision и SHA-256,
сначала проверяется `bproxy serve --test`, затем применяется через локальный
Unix gRPC core. При несовместимом изменении агент перезапускает core, а при
неудаче восстанавливает last-known-good config.

Статистика идёт двумя раздельными потоками, которые нельзя складывать:

1. `InterfaceTrafficBatch` — RX/TX bytes, packets, errors и drops выбранных
   интерфейсов network namespace контейнера ноды. Сюда входит payload,
   Board/API-транспорт, gRPC control traffic и прочий overhead контейнера.
2. `UserTrafficBatch` — логический расшифрованный payload, атрибутированный core
   по `user_tag`; RX означает upload от клиента, TX — download к клиенту.

Node-agent считает дельты от kernel/core cumulative counters. Checkpoint и
outbox event фиксируются одной SQLite-транзакцией в WAL с `synchronous=FULL`;
хаб сначала идемпотентно
сохраняет batch по UUID и только потом ACK-ает. После reconnect неподтверждённые
batch отправляются повторно. Первый interface sample задаёт baseline, reset
счётчика/рестарт core начинает новую эпоху без отрицательных дельт.
HTTP `/healthz` проверяет только жизнь процесса, а `/readyz` возвращает 200,
только когда активна хотя бы одна включённая доска.

Кроме telemetry batches node-agent держит отдельный server-streaming gRPC
канал к core для runtime events. Cursor и событие попадают в SQLite outbox
атомарно, а подключённый hub stream просыпается сразу — polling не участвует в
обычной доставке. При потере части bounded-журнала core явно сообщает reset,
после чего node-agent отправляет авторитетный runtime snapshot. Hub применяет
его как replacement projection и затем принимает только более новые sequence.

Control-plane является владельцем per-node агрегата `Node + Board + User +
NodeAssignment`. Ресурсы имеют optimistic version и состояния `enabled`,
`disabled`, `revoked`; отзыв терминален, а секрет отозванного пользователя не
попадает в следующий core snapshot. Каждая принятая mutation валидирует весь
агрегат, детерминированно компилирует TOML и создаёт immutable revision с
SHA-256 и ссылкой на предыдущую. Node status отдельно проецирует online,
readiness, последний ApplyResult и desired/applied drift. In-process event bus
будит подключённую ноду сразу, периодический reconcile страхует потерянные и
cross-process уведомления.

Активный Kotlin control-plane сохраняет catalog, encrypted revision, audit и
outbox атомарно в PostgreSQL. Он также хранит одноразовые enrollment tokens,
node status, два независимых дерева traffic deltas и сырые runtime event batches.
Application layer зависит от узких портов; gRPC, REST, SQL, PKI и TOML остаются
во внешних адаптерах. Go filesystem backend сохранён только как migration
reference и Compose его не запускает. Core при этом остаётся disposable.

## 5. Практический маршрут отладки

Для проблем подключения включить `--debug` и идти по контрольным точкам:

1. `yandex.Join` получил guest token, board info и Socket.IO соединение;
2. hub увидел HELLO, пользователь активен, `pagePool.acquire` успешен;
3. обе стороны залогировали assigned page, `Stats().FreePages` уменьшился;
4. `link stats`: `peer_rwnd > 0`, меняются `inflight` и RTT, heartbeat доходит;
5. `mux` создаёт SYN и stream отражается в gRPC/HTTP runtime statistics;
6. `egress` успешно делает dial до `Target()`;
7. при shutdown наблюдаются GOAWAY/`mux.Done`, статус `reconnecting`, а
   `FreePages` возвращается после `watchClient`;
8. при кратком WebSocket-обрыве mux не закрывается, но приходит reconnect
   snapshot и выполняется reconcile.
9. UDP проверяется через SOCKS5 UDP ASSOCIATE: локальный relay возвращает bound
   address, mux сохраняет границу сообщения, egress UDP socket возвращает ответ
   с исходным source address.

Основные проверки: `go test ./...` отдельно в `core` и `node-agent`, затем
`go test -race ./...`; standalone Go protobuf SDK находится в
`control-plane/contracts/gen/go`. Для control-plane — `./gradlew clean test
bootJar` в `control-plane/server`. Live-тесты Yandex включаются переменными,
описанными в `core/internal/boardtest` и `core/README.md`.
