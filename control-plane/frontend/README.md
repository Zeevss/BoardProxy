# BoardProxy Control Plane UI

React/TypeScript operations UI for the Kotlin control-plane. The production
Docker image builds it into Spring Boot static resources, so browser API and
SSE calls remain same-origin. Development Vite proxies `/api` and `/actuator`
to `localhost:8080`.

```bash
npm ci --include=dev
npm run test
npm run lint
npm run build
npm run dev
```

The bearer token is stored in `sessionStorage`, never persistent browser
storage. Runtime activity uses an authenticated fetch stream because native
`EventSource` cannot attach the Authorization header. All desired-state
mutations include the current catalog version in `If-Match`; a concurrent edit
is surfaced rather than overwritten.

The UI deliberately displays interface traffic and per-user payload as separate
series. User private keys are write-only and are never returned by the API.
