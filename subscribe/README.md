# BoardProxy Subscribe

Первая итерация сервиса подписок. Сам `subscribe` не хранит состояние: при запуске он получает TOML-конфигурацию, адрес control-plane и редакторскую ссылку на одну Яндекс Таблицу. Подписки, соответствия ключей и счётчики остаются в PostgreSQL control-plane.

## Потоки получения

1. Клиент получает URL `https://subscribe.example.com/s/<token>#bp1=<capsule>`.
2. Браузер показывает HTML-страницу, список ключей, трафик, QR и ссылки на приложения.
3. SDK отправляет тот же запрос с `Accept: application/vnd.boardproxy.subscription+json` и получает массив `keys[]`.
4. Только при сетевой ошибке или HTTP 5xx SDK открывает Яндекс Таблицу, размещает `BP1` hello в свободной ячейке и выполняет Noise IK с отдельными recovery-ключами.
5. При неудаче обоих каналов SDK может вернуть сохранённый last-known-good snapshot. HTTP 4xx считается окончательным ответом и не обходится через резервный канал.

Фрагмент после `#` не попадает в обычный HTTP GET и access-логи. В нём находятся редакторская ссылка, приватный recovery-ключ конкретной подписки и публичный recovery-ключ сервера. Ответы в комментариях зашифрованы и аутентифицированы Noise IK; сама таблица не является доверенным хранилищем. В первой итерации кнопка QR отправляет полный URL в теле POST на доверенный `subscribe` (не в URL запроса); этот endpoint не должен логировать тела запросов.

## Настройка

Скопируйте `config.example.toml` в `config.toml` и задайте:

- отдельный API-токен control-plane с ролью `SUBSCRIBER`;
- общую редакторскую ссылку Яндекс Таблицы;
- отдельный 32-байтовый recovery private key сервиса;
- публичный URL сервиса.

Сгенерировать recovery private key можно, например, так:

```bash
openssl rand -base64 32 | tr '+/' '-_' | tr -d '='
```

Запуск локально:

```bash
go run ./cmd/subscribe -config ./config.toml
```

Публичный recovery-ключ для настройки control-plane можно получить без запуска HTTP-сервера и watcher-а:

```bash
go run ./cmd/subscribe -config ./config.toml -print-public-key
```

Или через opt-in профиль Compose:

```bash
docker compose --profile subscribe up --build
```

`GET /readyz` отражает готовность основного HTTP-канала. `GET /recoveryz` отдельно показывает состояние watcher-а Яндекс Таблицы, поэтому отказ резервного канала не исключает исправный HTTP-сервис из балансировки.

## Обычное создание пользователя

Когда подписки включены, основной endpoint создания пользователя — `POST /api/v1/users`. Он атомарно создаёт одного пользователя на всех указанных нодах, добавляет назначения и возвращает одну ссылку подписки. Одна подписка содержит по одному ключу для каждой целевой ноды:

```http
POST /api/v1/users
Authorization: Bearer <operator-token>
Content-Type: application/json

{
  "id": "alice",
  "name": "Alice",
  "targets": [
    {"nodeId":"node-1","boardIds":["board-1"],"keyName":"Германия"},
    {"nodeId":"node-2","boardIds":["board-2"],"keyName":"Нидерланды"}
  ],
  "maxSessions": 2,
  "maxLanes": 3
}
```

При `CONTROL_SUBSCRIPTION_ENABLED=true` ответ содержит только одну точку доставки:

```json
{
  "id": "alice",
  "name": "Alice",
  "deliveryType": "subscription",
  "subscriptionId": "...",
  "subscriptionUrl": "https://subscribe.example.com/s/...#bp1=...",
  "keys": []
}
```

При выключенном флаге тот же endpoint сохраняет прежний режим и возвращает `deliveryType: "keylinks"` с массивом ключей. Старый `PUT /api/v1/nodes/{nodeId}/users/{userId}` остаётся низкоуровневой административной операцией; интерфейс control-plane использует новый агрегирующий endpoint.

Для автоматической выдачи ссылки control-plane и `subscribe/config.toml` должны описывать одинаковые `public URL`, URL Яндекс Таблицы, `recovery key ID` и recovery server public key:

```dotenv
CONTROL_SUBSCRIPTION_ENABLED=true
CONTROL_SUBSCRIPTION_PUBLIC_URL=https://subscribe.example.com
CONTROL_SUBSCRIPTION_YANDEX_EDITOR_URL=https://disk.yandex.ru/i/replace-me
CONTROL_SUBSCRIPTION_RECOVERY_KEY_ID=recovery-2026-01
CONTROL_SUBSCRIPTION_RECOVERY_SERVER_PUBLIC_KEY=<output-of-print-public-key>
```

## Низкоуровневое управление подписками

Сначала администратор выпускает служебный токен для `subscribe`:

```http
POST /api/v1/access/tokens
Authorization: Bearer <admin-token>
Content-Type: application/json

{"name":"subscribe-service","role":"SUBSCRIBER"}
```

Для ручного администрирования оператор может создать подписку отдельно. Каждая запись в `keys` ссылается на существующую пару `nodeId/userId`; порядок массива сохраняется в выдаче:

```http
POST /api/v1/subscriptions
Authorization: Bearer <operator-token>
Content-Type: application/json

{
  "name": "Family",
  "keys": [
    {"id":"phone","name":"Телефон","nodeId":"node-1","userId":"alice"},
    {"id":"laptop","name":"Ноутбук","nodeId":"node-2","userId":"alice"}
  ]
}
```

Ответ содержит `token` и `recoveryClientPrivateKey` только при создании. Control-plane хранит SHA-256 токена и только публичный recovery-ключ клиента.

Полный URL собирается процессом `subscribe`, которому известны параметры резервного канала. JSON-ответ создания передаётся через stdin, чтобы секреты не попадали в argv и список процессов:

```bash
curl -sS -X POST https://control.example.com/api/v1/subscriptions \
  -H "Authorization: Bearer $OPERATOR_TOKEN" \
  -H 'Content-Type: application/json' \
  --data @subscription.json \
| go run ./cmd/subscribe -config ./config.toml -print-url
```

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
