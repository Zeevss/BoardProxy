# BoardProxy control UI design system

Source of truth: `design/fleet-overview-concept.png` (1440 x 1000).

- Background is neutral near-black `#090e12`; panels use `#10171d` and thin
  `#27323b` dividers. There are no gradients, glass effects or floating card grid.
- Primary accent is cyan `#26b8f3`; health is `#35d487`, warning `#ffb326`,
  critical `#ff5c5c`.
- Typography is Inter/system sans. Page title 30/36 semibold, section title
  17/24 semibold, body 14/21, UI chrome 12/18.
- Layout uses a 194px sidebar, 64px top bar, 16px content gaps and at most 8px
  radii. Desktop panels become a single column below 980px; sidebar collapses
  below 760px.
- Component families: app shell, navigation rows, status rail, segmented
  control, line chart, data table, alert line, text/button/form primitives.
- Allowed first-viewport copy: BoardProxy, Control Plane, Overview, Nodes,
  Users, Boards, Traffic, Access, Fleet overview, Selected node, Live,
  Event stream, Connected, Interface traffic, User payload, Runtime state,
  User sessions, Board lifecycle, Recent activity.
- All controls and text remain code-native. Icons are consistent 20px outline
  SVGs with 1.8px strokes.
