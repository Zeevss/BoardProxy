# --- Сборка единого бинарника bproxy ---
FROM golang:1.26-alpine AS build
WORKDIR /src

# Зависимости отдельным слоем — кэшируются, пока не менялись go.mod/go.sum.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Core has no database or CGO dependency, so the runtime image contains only
# the static binary and CA certificates.
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/bproxy ./cmd/bproxy

# --- Рантайм ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 bproxy
COPY --from=build /out/bproxy /usr/local/bin/bproxy

RUN mkdir -p /run/bproxy && chown bproxy:bproxy /run/bproxy
VOLUME /run/bproxy
USER bproxy

ENTRYPOINT ["bproxy"]
