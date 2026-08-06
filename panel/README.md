# BoardProxy multi-node panel

Панель — самостоятельный control-plane, не привязанный к одному процессу core.
Она состоит из React UI и небольшого Go gateway. Gateway хранит список нод и
их access keys в `/data/nodes.json` с правами `0600`; ключи никогда не уходят в
браузер. Все прежние экраны (`Clients`, `Boards`, `Statistics`, `Logs`,
`Maintenance`) работают относительно выбранной ноды.

## Подключение ноды

На каждой ноде management API должен слушать TCP-порт:

```sh
./bproxy serve --web-api=0.0.0.0:8080
```

Сгенерируйте отдельный ключ для панели:

```sh
./bproxy serve keygen panel
./bproxy serve keys
./bproxy serve revoke <id>
```

`keygen` показывает секрет один раз. В БД ноды хранится только SHA-256 digest;
ключей может быть несколько, отзыв применяется к следующему HTTP-запросу без
перезапуска core. В UI откройте «Ноды», укажите название, IP/hostname, порт и
полученный `bpa_…` ключ.

## Docker Compose

```sh
cp .env.example .env
docker compose up -d --build
docker compose exec core bproxy serve keygen panel --db /data/bproxy.db
```

Панель доступна на `PANEL_PORT`. Для ноды из того же compose используйте host
`core`, port `8080`. Порт `CORE_API_PORT` по умолчанию публикуется только на
`127.0.0.1`; для удалённой панели откройте management API через firewall и TLS
reverse proxy, затем отметьте HTTPS при добавлении ноды.

Проверить ключ напрямую на хосте ноды можно так:

```sh
read -rsp 'Core access key: ' BPROXY_CORE_KEY; echo
curl -i -H "Authorization: Bearer ${BPROXY_CORE_KEY}" http://127.0.0.1:8081/stats
unset BPROXY_CORE_KEY
```

Ответ `200 OK` подтверждает доступ. `401 Unauthorized` означает, что ключ
создан в другой БД, отозван либо core ещё запущен со старой версией без
поддержки отзываемых access keys.

Данные панели находятся в отдельном томе `panel-data`, данные ноды — в
`bproxy-data`. Контейнеры можно запускать и обновлять независимо.

## Разработка

```sh
npm install
npm run build
cd gateway && go test ./...
```

Vite UI обращается к gateway по same-origin `/api`. `/api/nodes` управляет
реестром, а `/api/node/*` проксируется в выбранный core с bearer-ключом.
