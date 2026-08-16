# BoardProxy Control Plane frontend

React/TypeScript operator console for the Kotlin control-plane. The production
Docker build copies `dist/` into Spring Boot static resources, so REST and SSE
requests stay same-origin.

```bash
npm ci --include=dev
npm run test
npm run lint
npm run build
npm run dev
```

Vite proxies `/api` and `/actuator` to `http://localhost:8080` in development.
The bearer token is kept in `sessionStorage`; API writes use the current entity
version through `If-Match`. Subscription, enrollment and API-token secrets are
shown only from the creation response and are never persisted by the UI.

The interface follows the supplied `Control Plane v1.dc.html` design: neutral
near-black surfaces, compact IBM Plex typography, dense tables, thin borders and
green/violet traffic accents. Desktop and mobile layouts are both supported.
