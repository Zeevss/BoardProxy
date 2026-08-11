FROM golang:1.26-alpine AS build
WORKDIR /src/control-plane/backend

COPY control-plane/backend/go.mod control-plane/backend/go.sum ./
RUN go mod download
COPY control-plane/backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/bproxy-hub ./cmd/bproxy-hub

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 bproxy
COPY --from=build /out/bproxy-hub /usr/local/bin/bproxy-hub
RUN mkdir -p /var/lib/bproxy-hub && chown bproxy:bproxy /var/lib/bproxy-hub
VOLUME /var/lib/bproxy-hub
USER bproxy
ENTRYPOINT ["bproxy-hub"]
