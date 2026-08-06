# --- Сборка единого бинарника bproxy ---
FROM golang:1.26-alpine AS build
WORKDIR /src

# Зависимости отдельным слоем — кэшируются, пока не менялись go.mod/go.sum.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# modernc.org/sqlite — чистый Go, поэтому CGO не нужен: собираем статический
# бинарник и кладём его в минимальный образ.
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/bproxy ./cmd/bproxy

# --- Рантайм ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 bproxy
COPY --from=build /out/bproxy /usr/local/bin/bproxy

# /data — том с БД и приватным ключом сервера (переживает пересборку контейнера).
RUN mkdir -p /data && chown bproxy:bproxy /data
VOLUME /data
USER bproxy

# Управляющий web-api. Публикуйте его только в доверенной сети либо за TLS;
# доступ панели авторизуется отзываемым bearer-ключом из `serve keygen`.
EXPOSE 8080
ENTRYPOINT ["bproxy"]
