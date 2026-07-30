# --- Сборка статики панели ---
FROM node:20-alpine AS build
WORKDIR /app

# Ставим зависимости отдельным слоем — переиспользуется, пока не менялся lock.
COPY package.json package-lock.json* ./
RUN npm install

COPY . .
RUN npm run build

# --- Рантайм: Caddy отдаёт статику и проксирует /api в core ---
FROM caddy:2-alpine
COPY Caddyfile /etc/caddy/Caddyfile
COPY --from=build /app/dist /srv
EXPOSE 80
