FROM golang:1.26-alpine AS build
WORKDIR /src

COPY control-plane/contracts/gen/go/go.mod control-plane/contracts/gen/go/go.sum ./control-plane/contracts/gen/go/
COPY subscribe/go.mod subscribe/go.sum ./subscribe/
COPY core/go.mod core/go.sum ./core/
COPY node-agent/go.mod node-agent/go.sum ./node-agent/
RUN cd node-agent && GOWORK=off go mod download
RUN cd core && GOWORK=off go mod download

COPY control-plane/contracts/gen/go/ ./control-plane/contracts/gen/go/
COPY subscribe/ ./subscribe/
COPY core/ ./core/
COPY node-agent/ ./node-agent/
RUN cd core && CGO_ENABLED=0 GOWORK=off go build -trimpath -ldflags "-s -w" -o /out/bproxy ./cmd/bproxy
RUN cd node-agent && CGO_ENABLED=0 GOWORK=off go build -trimpath -ldflags "-s -w" -o /out/bproxy-node ./cmd/bproxy-node

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 bproxy
COPY --from=build /out/bproxy /usr/local/bin/bproxy
COPY --from=build /out/bproxy-node /usr/local/bin/bproxy-node
RUN mkdir -p /var/lib/bproxy-node /run/bproxy \
    && chown -R bproxy:bproxy /var/lib/bproxy-node /run/bproxy
VOLUME /var/lib/bproxy-node
USER bproxy
ENTRYPOINT ["bproxy-node"]
