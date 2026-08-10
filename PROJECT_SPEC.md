# BoardProxy: краткая спецификация проекта

## 1. Назначение и состав

BoardProxy — SOCKS5 TCP/UDP и HTTP-прокси, у которого транспортом служат текстовые объекты
на страницах Yandex Board. Создание объекта передаёт пакет, удаление объекта
подтверждает его получение. Репозиторий состоит из двух самостоятельных частей:

- `core/` — Go-бинарник `bproxy`: клиент, сервер и management API;
- `panel/` — самостоятельная React/Go multi-node панель: gateway хранит реестр
  нод и старый HTTP API; после stateless-рефакторинга он считается legacy и не
  включён в штатный Compose до появления gRPC control-plane адаптера;
- `docker-compose.yml` — запуск stateless core с read-only TOML и эфемерным
  Unix gRPC-сокетом.

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

## 4. Desired state, gRPC и статистика

TOML — единственный долговечный desired state core. gRPC применяет изменения к
живому runtime без рестарта: пользовательская policy публикуется атомарно,
затронутые сессии закрываются, доски независимо добавляются, отключаются или
заменяются. Доска с временно недоступным API переходит в `retrying` и сама
восстанавливается с ограниченным exponential backoff. `ApplySnapshot` атомарно
заменяет весь срез пользователей и досок. Каждая mutation проверяет
`expected_revision`. Изменения gRPC не
записываются обратно: следующий `Reload` снова делает файл источником истины.

Core не хранит историческую статистику. Он отдаёт active users/connections/
lanes/streams, payload и network bytes since start, состояние pool/cleanup и
transport reconnect counters. Для истории внешний control plane должен
периодически scrape-ить gRPC `GetStats` или read-only `GET /stats`.
HTTP `/healthz` проверяет только жизнь процесса, а `/readyz` возвращает 200,
только когда активна хотя бы одна включённая доска.

Старая панель опирается на удалённые SQLite, HTTP CRUD, backup и restart
handlers. Она оставлена в репозитории как отдельный legacy-компонент, но не
подключена к Compose. Масштабируемая замена должна владеть durable config,
секретами и аудитом, а core — оставаться disposable runtime.

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

Основные проверки: `cd core && make test` (или `go test ./...`), отдельно
`go test -race ./...`; для панели — `cd panel && npm run build`. Live-тесты
Yandex включаются переменными, описанными в `core/internal/boardtest` и
`core/README.md`.
