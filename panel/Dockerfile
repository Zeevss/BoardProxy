# --- Сборка статики панели ---
FROM node:20-alpine AS build
WORKDIR /app

# Ставим зависимости отдельным слоем — переиспользуется, пока не менялся lock.
COPY package.json package-lock.json* ./
RUN npm install

COPY . .
RUN npm run build

# --- Standalone control-plane gateway ---
FROM golang:1.26-alpine AS gateway
WORKDIR /src
COPY gateway/go.mod ./
COPY gateway/main.go ./
RUN CGO_ENABLED=0 go build -trimpath -o /panel-gateway .

FROM alpine:3.22
RUN addgroup -S panel && adduser -S -G panel panel && mkdir -p /data /srv && chown panel:panel /data
COPY --from=gateway /panel-gateway /usr/local/bin/panel-gateway
COPY --from=build /app/dist /srv
USER panel
ENV PANEL_ADDR=:8080 PANEL_STATIC=/srv PANEL_DATA=/data/nodes.json
EXPOSE 8080
ENTRYPOINT ["panel-gateway"]
