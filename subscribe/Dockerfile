# Сборка нативная, целевую архитектуру Go берёт на себя. Рантайм здесь
# distroless и без единого RUN, так что эмуляции этот образ не требует вовсе.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETARCH
# Версию сервис сообщает control-plane, и она видна в панели.
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /out/subscribe ./cmd/subscribe

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/subscribe /subscribe
EXPOSE 8090
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=6 \
  CMD ["/subscribe", "-healthcheck", "http://127.0.0.1:8090/readyz"]
ENTRYPOINT ["/subscribe", "-config", "/etc/boardproxy-subscribe/config.toml"]
