# BoardProxy Control Plane Frontend

The frontend is intentionally not implemented yet. This directory marks a
separate deployable component, following the useful Remnawave boundary between
backend and frontend without coupling the browser to the node protocol.

The future UI will consume a versioned HTTP API exposed by `backend`; it will
never call node gRPC directly. Before choosing a frontend stack we will define
the panel read models and commands together:

- overview and alerts;
- nodes, connectivity and applied/desired revision drift;
- users, boards and config revisions;
- interface traffic and per-user payload as separate charts;
- enrollment, certificate state and audit events;
- operational actions with explicit confirmation and RBAC.

No REST DTOs are frozen yet: the backend currently exposes only the stable node
contract. This avoids designing the API around an imagined UI.
