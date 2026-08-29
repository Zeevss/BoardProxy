# BoardProxy Subscribe

Сервис подписок. `subscribe` не хранит состояние: он знает только адрес control-plane и свой токен, а конфигурацию забирает по этому каналу. Настройки, подписки, соответствия ключей и счётчики живут в PostgreSQL control-plane.

## Потоки получения

1. Клиент получает URL `https://subscribe.example.com/s/<token>#bp1=<capsule>`.
2. Браузер показывает HTML-страницу, список ключей, трафик, QR и ссылки на приложения.
3. SDK отправляет тот же запрос с `Accept: application/vnd.boardproxy.subscription+json` и получает массив `keys[]`.
4. Только при сетевой ошибке или HTTP 5xx SDK открывает Яндекс Таблицу, размещает `BP1` hello в свободной ячейке и выполняет Noise IK с отдельными recovery-ключами.
5. При неудаче обоих каналов SDK может вернуть сохранённый last-known-good snapshot. HTTP 4xx считается окончательным ответом и не обходится через резервный канал.

Фрагмент после `#` не попадает в обычный HTTP GET и access-логи. В нём находятся редакторская ссылка, приватный recovery-ключ конкретной подписки и публичный recovery-ключ сервера. Ответы в комментариях зашифрованы и аутентифицированы Noise IK; сама таблица не является доверенным хранилищем. В первой итерации кнопка QR отправляет полный URL в теле POST на доверенный `subscribe` (не в URL запроса); этот endpoint не должен логировать тела запросов.

## Настройка

Сервис знает о себе только три вещи: где хаб, какой у него токен и какой порт
слушать. Публичный URL, ссылка на Яндекс Таблицу, recovery-ключ и ссылки на
клиенты задаются в панели control-plane и приезжают в сервис автоматически.

1. В панели: **Настройки → Система подписок → Выпустить токен**. Секрет
   показывается один раз; перевыпуск немедленно отзывает предыдущий.
2. Впишите его в корневой `.env`:

```dotenv
BPROXY_SUBSCRIBE_CONTROL_TOKEN=bpat_...
```

3. Запустите сервис:

```bash
docker compose --profile subscribe up -d --build subscribe
```

Как только сервис придёт за конфигурацией, в панели он отметится подключённым и
настройки разблокируются. Заполните их и нажмите «Сохранить» — правка доедет в
течение опроса (15 секунд), передеплой не нужен. Кнопка «Перезапустить сервис»
отдаётся тем же каналом ровно один раз, и контейнер поднимается заново своим
`restart: unless-stopped`.

Recovery-пару генерирует control-plane при первом сохранении настроек и хранит
приватный ключ зашифрованным мастер-ключом. Прежней процедуры с
`-print-public-key` и ручной сверкой ключей в двух `.env` больше нет.

Локальный запуск без Compose:

```bash
cp config.example.toml config.toml   # укажите url и token
go run ./cmd/subscribe -config ./config.toml
```

## Как сервис получает конфигурацию

Один POST-запрос делает три вещи сразу:

```http
POST /api/v1/subscription-service/poll
Authorization: Bearer bpat_...
{"revision": 7, "serviceVersion": "1.0.0", "recoveryWatcherReady": true}
```

Он сообщает состояние сервиса, забирает свежую конфигурацию и узнаёт о
запрошенном перезапуске. `204 No Content` означает «у тебя уже актуально».
Отдельного heartbeat нет: сам этот запрос им и является, поэтому «подключен» в
панели означает «приходил за конфигурацией не позже 45 секунд назад».

Подтверждение перезапуска ведёт control-plane, а не сервис. Сервис
stateless и после рестарта отчитался бы нулём, получив ту же команду снова —
и так бесконечно. Поэтому перезапуск отдаётся ровно один раз; потерянная
доставка лечится повторным нажатием, а не циклом.

## Создание пользователя и подписки

Пользователь, его размещения и подписка — три явных ресурса. Сначала создайте
пользователя:

```http
POST /api/v1/users
Authorization: Bearer <operator-token>
Content-Type: application/json

{
  "id": "alice",
  "name": "Alice",
  "maxSessions": 2,
  "maxLanes": 3
}
```

Затем одной заменой задайте его размещения:

```http
PUT /api/v1/users/alice/grants
Authorization: Bearer <operator-token>
Content-Type: application/json

[
  {"nodeId":"node-1","boardIds":["board-1"]},
  {"nodeId":"node-2","boardIds":["board-2"]}
]
```

После этого создайте подписку через `POST /api/v1/subscriptions`. Она не
копирует список нод: `keys[]` в resolve всегда выводится из актуальных grants
пользователя. При глобальном выключении сервиса старые ссылки также перестают
резолвиться до повторного включения.

Отдельно согласовывать `public URL`, ссылку на Яндекс Таблицу и recovery-ключи между двумя `.env` больше не нужно: у них один владелец — control-plane.

## Низкоуровневое управление подписками

Токен сервиса выпускается в панели (Настройки → Система подписок), а не вручную через `/api/v1/access/tokens`: панель заодно отзывает предыдущий и запоминает выданный.

Для ручного администрирования оператор создаёт подписку на одного существующего
пользователя:

```http
POST /api/v1/subscriptions
Authorization: Bearer <operator-token>
Content-Type: application/json

{
  "name": "Family",
  "userId": "alice"
}
```

Ответ содержит `token`, `recoveryClientPrivateKey` и готовый `url`. Control-plane хранит SHA-256 токена для резолва, а сам токен и приватный recovery-ключ — зашифрованными мастер-ключом (AES-256-GCM, как приватные ключи пользователей). Поэтому ссылка постоянная: её можно получить снова через `GET /api/v1/subscriptions/{id}/link` (роль `OPERATOR`/`ADMIN`), а не только в момент создания.

`POST /api/v1/subscriptions/{id}/rotate` с `If-Match` выпускает новый токен и новую recovery-пару. Прежняя ссылка перестаёт действовать немедленно.

Подписки, выпущенные до этой возможности, секретов не хранят: у них `link` вернёт `null`, и рабочую ссылку даёт только ротация.

Изменение выполняется `PUT /api/v1/subscriptions/{id}` с заголовком `If-Match: "<version>"`. Состояния: `enabled`, `disabled`, `revoked`.

## Go SDK

```go
import "github.com/Zeevss/BoardProxy/subscribe/sdk"

client := &sdk.Client{
    HTTP:  &http.Client{Timeout: 10 * time.Second},
    Cache: sdk.NewMemoryCache(),
}
snapshot, err := client.Fetch(ctx, subscriptionURL)
for _, key := range snapshot.EnabledKeys() {
    // key.ID стабилен, key.Keylink можно передать BoardProxy client.
}
```

У вызывающего контекста должен быть deadline, особенно для резервного канала.

## Локальный просмотр и тесты

Демонстрационная HTML-страница с тремя ключами запускается без control-plane и Яндекс Таблицы:

```bash
go run ./cmd/preview
# http://127.0.0.1:8091/s/preview
```

Основные проверки:

```bash
# subscribe, SDK и protocol
go test -race ./...
go vet ./...

# control-plane
../control-plane/server/gradlew -p ../control-plane/server test

# frontend
npm --prefix ../control-plane/frontend test -- --run
npm --prefix ../control-plane/frontend run build
npm --prefix ../control-plane/frontend run lint
```

Live E2E создаёт временный тред в указанной таблице, проверяет WebSocket-события, Noise IK и выдачу нескольких ключей, затем удаляет тред:

```bash
YANDEX_SHEETS_E2E_URL='https://disk.yandex.ru/i/your-sheet' \
go test -v ./internal/yandex ./sdk \
  -run 'TestLive(CommentEventsAndCleanup|YandexRecoveryReturnsMultipleKeys)' -count=1
```

## Границы первой итерации

- Один watcher обслуживает одну таблицу.
- WebSocket используется как уведомление; после разрыва сервис заново открывает сессию и сверяет snapshot/history.
- Сторонний редактор не может подделать корректный зашифрованный ответ, но может постоянно удалять комментарии и тем самым устроить DoS резервного канала. Это ограничение общей редакторской ссылки, а не устранимая криптографией проблема.
- Обновление TOML и ротация server recovery key применяются полным перезапуском. Старые URL после удаления старого ключа перестанут восстанавливаться через Яндекс.
