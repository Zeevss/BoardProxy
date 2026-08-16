# BoardVPN Android architecture

## Purpose

BoardVPN is an Android VPN client around the BoardProxy Go core distributed as
an AAR. The application must expose a reactive UI, keep the VPN session alive
independently of an Activity, and support deterministic reconnect, shutdown,
TCP and UDP forwarding.

## Architectural rules

The application uses three explicit layers plus an application composition
root:

```text
ui -> domain <- infrastructure
        ^
        |
       app
```

- `ui` contains Compose screens, routes, ViewModels, navigation, UI state and
  user actions. It may depend only on `domain` and Android UI APIs.
- `domain` contains models, repository contracts and the pure VPN state
  transition logic. It must not depend on Android, Compose, AAR/gomobile, TUN
  file descriptors or concrete persistence. Dedicated use-case classes are
  introduced only when logic is complex or reused; simple repository calls are
  not wrapped in one-method classes.
- `infrastructure` implements domain contracts using `VpnService`, the
  BoardProxy AAR, TUN/tun2socks, DataStore and Android notifications.
- `app` is not a business layer. It contains Android entry points and the
  composition root that wires interfaces to implementations.

The dependency direction is mandatory:

```text
ui             -> domain
infrastructure -> domain
app            -> ui + domain + infrastructure
domain         -> nothing application-specific
```

Direct dependencies from `ui` to `infrastructure`, and all dependencies from
`domain` to Android or infrastructure, are forbidden.

## Package structure

```text
ru.zevsus.proxy.boardvpn
|-- app
|   |-- BoardVpnApplication.kt
|   |-- AppContainer.kt
|   `-- MainActivity.kt
|-- ui
|   |-- home
|   |   |-- HomeRoute.kt
|   |   |-- HomeScreen.kt
|   |   |-- HomeViewModel.kt
|   |   |-- HomeUiState.kt
|   |   `-- ConnectionButton.kt
|   |-- profiles
|   |   |-- ProfilesRoute.kt
|   |   |-- ProfilesScreen.kt
|   |   |-- ProfilesViewModel.kt
|   |   |-- ProfilesUiState.kt
|   |   `-- ProfileEditorDialog.kt
|   |-- settings
|   |   |-- SettingsRoute.kt
|   |   |-- SettingsScreen.kt
|   |   `-- SettingsViewModel.kt
|   |-- scanner
|   |-- navigation
|   |   |-- BoardVpnApp.kt
|   |   `-- BoardVpnDestination.kt
|   |-- components
|   `-- theme
|-- domain
|   |-- model
|   |-- repository
|   `-- logic
`-- infrastructure
    |-- fake
    |-- vpn
    |   |-- service
    |   `-- permission
    |-- core
    |-- tun
    `-- persistence
```

Packages are introduced only when they contain real code. Empty marker classes
or interfaces must not be created merely to make the directory tree visible.

## Runtime ownership

`BoardVpnService` owns the complete VPN runtime:

- the TUN `ParcelFileDescriptor`;
- the BoardProxy AAR client;
- the tun2socks engine;
- reconnect jobs and timers;
- foreground notification lifecycle;
- graceful and forced shutdown.

Compose and ViewModels never own or call the AAR client directly. UI sends a
domain command and observes immutable state:

```text
Compose -> ViewModel -> VpnRepository -> infrastructure implementation
                                        |
                                        v
                                 BoardVpnService
                                        |
                       AndroidVpnRuntime + AAR + TUN
                                        |
                                        v
VpnRepository.observeSession() -> Flow -> ViewModel -> Compose
```

Callbacks from gomobile may arrive from arbitrary native threads. They must be
tagged with a session ID and forwarded to a single runtime event queue. Only
that queue may perform state transitions and order resource operations.

## State requirements

The VPN session and BoardProxy transport connection are different concepts. A
connected core does not make the complete VPN session connected until TUN and
tun2socks are ready.

The first version supports the following session phases:

```text
Idle
Starting
RequestingTunnel
ConnectingCore
StartingTun
Connected
Reconnecting
Stopping
Failed
```

Low-frequency session state and frequently changing traffic statistics use
separate `StateFlow` instances to avoid unnecessary whole-screen recomposition.
Transient state, file descriptors, AAR instances and current statistics are not
persisted.

The UI does not mirror every runtime phase. `HomeViewModel` projects detailed
domain state into five display statuses:

```text
Idle / Failed                                      -> Disconnected
Starting / RequestingTunnel / ConnectingCore /
StartingTun                                        -> Connecting
Connected                                          -> Connected
Reconnecting                                       -> Reconnecting
Stopping                                           -> Disconnecting
```

A failure is a separate UI field rather than another whole-screen mode. This
keeps rendering and button rules small while preserving detailed runtime state
for logging, tests and correct resource ordering.

Every resource release operation must be idempotent. A disconnect cancels
reconnect before stopping tun2socks, stopping the core, closing TUN, publishing
`Idle`, removing the foreground notification and stopping the service.

## Naming

- `VpnSession*` describes the complete Android VPN lifecycle.
- `BoardProxy*` describes the Go core and its transport connection.
- `*Repository` exposes observable state and explicit verb-named operations.
- `*Engine` owns an executable subsystem such as tun2socks.
- `Aar*`, `Android*`, `DataStore*` identify infrastructure implementations and
  must not appear in domain API names.

## Dependency policy

- Start with manual dependency injection through `AppContainer`.
- Do not add Hilt until the dependency graph and scopes make manual wiring
  materially difficult.
- Do not add Room while profiles fit DataStore or another small persistence
  mechanism.
- `androidx.navigation:navigation-compose` is used for the three top-level
  destinations; keep it on the 2.8 line while the Compose BOM stays at 2024.09.
- Icons come from `material-icons-core` plus the hand-drawn shield in
  `ui/components`; do not add `material-icons-extended`.
- Keep AAR/gomobile types inside `infrastructure/core`.
- Keep `Context`, `Intent`, `VpnService`, notifications and file descriptors out
  of domain.
- Domain models may carry `kotlinx.serialization` annotations, since the library
  is platform-neutral. Persistence file names, DataStore wiring and storage
  envelopes stay in `infrastructure/persistence`.
- Keep the tun2socks contract transport-neutral. UDP uses standard SOCKS5 UDP
  ASSOCIATE while core control sockets remain protected from the VPN route.
- Full keylinks, credentials and private keys must never be written to logs.

## Implementation roadmap

### Stage 0: project foundation

- Separate Lifecycle and coroutines versions in the version catalog.
- Verify compatible Kotlin, coroutines and Lifecycle versions.
- Create the `app`, `ui`, `domain` and `infrastructure` package convention.
- Place Android entry points in `app`.
- Add `BoardVpnApplication` and the manual `AppContainer`.
- Keep the Compose theme in `ui/theme`.
- Replace the generated greeting with a minimal Home screen.
- Verify debug compilation and unit tests.

### Stage 1: domain models and state machine

- Add `VpnProfileId`, `VpnProfile` and validated keylink representation.
- Add `VpnSessionId`, `VpnSessionPhase`, `VpnSessionState`, `VpnStatistics` and
  typed `VpnFailure`.
- Define runtime events and the pure `VpnSessionReducer`.
- Specify valid transitions, stale callback handling and idempotent commands.
- Cover the transition matrix with JVM unit tests.

### Stage 2: domain contracts

- Define `VpnRepository` and `VpnProfileRepository` with regular named
  functions.
- Let ViewModels call repositories directly for simple observe/connect,
  disconnect and profile operations.
- Add a dedicated use case later only when it combines repositories, contains
  reusable business rules or materially simplifies more than one consumer.
- Keep Android permission intents outside domain.

### Stage 3: fake infrastructure

- Add in-memory profile storage and a fake `VpnRepository`.
- Simulate connect, reconnect, failure and shutdown timings.
- Wire fake implementations through `AppContainer`.

### Stage 4: reactive UI

Status: implemented for the Home, Profiles and Settings destinations.

- Add `HomeRoute`, immutable `HomeUiState`, `HomeAction` and `HomeViewModel`.
- Combine session state, statistics and profiles with lifecycle-aware
  collection.
- Project detailed runtime phases into five compact display statuses.
- Keep screens stateless and cover key states with previews and tests.
- Routes own side effects: snackbars, dialogs, clipboard and permission
  callbacks. Screens receive state plus callbacks only.
- Reuse `BoardVpnPageHeader`, `BoardVpnSection`, `BoardVpnNavigationRow`,
  `BoardVpnPill` and the shared card geometry instead of recreating screen
  chrome in each destination.

The shell is a bottom `NavigationBar` with three type-safe destinations
(`HomeDestination`, `ProfilesDestination`, `SettingsDestination`) hosted by
`BoardVpnApp`, which also owns the shared `SnackbarHostState`. Tab switching
saves and restores per-tab state.

Home uses a single animated shield control. Its surface and icon respond to the
connection state, while a pulse communicates connection and reconnection work.
The session stores the monotonic timestamp at which TUN became active, so the
timer continues from the same value when the Activity/ViewModel is recreated.
The screen also contains a session timer
driven by an injectable `TimeSource`, live download/latency/upload cards and a
profile selector sheet.

Profiles supports subscription URLs and direct `bproxy://` keys. Every
subscription is rendered as one group containing safe key metadata; direct
keys stay in a separate section. The Subscribe SDK is exposed through the
gomobile facade. `SubscriptionSyncManager` is the single update path: it runs
once when the application process starts, every 15 minutes while that process
is alive, from the Profiles refresh actions, and immediately before a VPN
start. Concurrent refreshes are serialized. A successful response atomically
replaces the selected key and safe metadata and records the update time; a
periodic or manual failure preserves the last working key and exposes a compact
error state. With a multi-key subscription the client requests the previously
selected key ID and keeps it while it remains enabled; only removal or disable
falls back to the first enabled key returned by the server. This avoids
switching nodes merely because response ordering changed and leaves room for a
future explicit key selector. The pre-connect path accepts a result fetched in the last 30
seconds so cold-start auto-connect does not immediately duplicate the startup
request. A pre-connect failure remains strict and prevents starting a new
VPN session with a possibly revoked subscription. The interval is centralized
as `SubscriptionSyncManager.DEFAULT_INTERVAL_MINUTES`.

The Profiles screen uses the same outer card geometry for subscription groups
and direct keys. Primary content stays visible; add/import actions and
edit/share/delete actions live in compact overflow menus. Subscription rows
show only the name, synchronization state and flat key rows, with a visible
section-level refresh action and a per-subscription refresh menu item. Profiles
also supports QR scanning and QR generation: a subscription shares its source
URL, while a direct profile shares its keylink. Manual add, rename, replacement,
delete confirmation and clipboard import remain available. Settings covers theme mode,
connect-on-launch, a shortcut to the system VPN screen and version information.
Network settings (SOCKS port, UDP, local DNS, DNS server, bypass subnets and
bypass routes) are deliberately absent until they are wired into the runtime.
Settings links to a dedicated application-routing destination. The screen lists
launcher-visible installed applications without requesting
`QUERY_ALL_PACKAGES`, supports search and exposes all-apps, bypass-selected and
proxy-selected-only modes.

### Stage 5: VPN permission flow

- Wrap `VpnService.prepare()` in infrastructure with `VpnPermissionManager`.
- Intercept the visual Connect action at the `HomeRoute`/Activity boundary.
- If permission is already granted, forward Connect to `HomeViewModel`
  immediately.
- Otherwise launch the returned intent through the Activity Result API and
  forward Connect only after `RESULT_OK`.
- Represent denial as a UI problem without storing Android intents in domain,
  repositories or ViewModels.

### Stage 6: BoardVpnService shell

Status: implemented.

- Declare the service, binding permission, intent filter and current foreground
  service requirements in the manifest.
- Add explicit idempotent Connect and Disconnect service commands.
- Add foreground notification states, live download/upload speed, Pause/Resume
  and Finish actions. Pause performs a full runtime shutdown but deliberately
  keeps the foreground service and notification; Resume reconnects the stored
  profile. Finish and the regular in-app Disconnect path remove the foreground
  notification and stop the service.
- Model service ownership with one sealed `ServiceState` instead of several
  independent booleans. This makes `Running`, `Stopping`, `Paused` and
  `Resuming` mutually exclusive and keeps command guards close to transitions.
- Request `POST_NOTIFICATIONS` at connect time on Android 13+; denial does not
  block the VPN, but Android will hide its foreground notification from the
  notification drawer.
- Handle `onStartCommand`, `onRevoke` and `onDestroy`.
- Explicitly defer always-on support until restart semantics are implemented.

System entry points share the application-scoped `VpnRepository` and stored
profile selection:

- `BoardVpnTileService` exposes a shield Quick Settings tile. Android
  `INACTIVE`, `ACTIVE` and `UNAVAILABLE` states represent disconnected,
  connected and connection/stop transitions. Starting from the tile is direct
  when VPN consent already exists; otherwise it opens `MainActivity` to request
  notification and VPN permissions.
- A long press on the Tile resolves `ACTION_QS_TILE_PREFERENCES` to
  `MainActivity` instead of Android's application-info screen.
- The static launcher shortcut `toggle_proxy` appears on long press as
  “Toggle proxy” / “Включить/выключить прокси”. It targets the translucent
  `ProxyToggleActivity` trampoline, toggles the repository and finishes without
  opening the main application task. Android's VPN consent dialog is the only
  UI it can display when consent has not been granted yet.
- The tile uses Android's standard non-active mode: the system binds it while
  Quick Settings is visible, and it collects the same session flow as the
  screen. This avoids stale `ACTIVE` state after process death and keeps the
  VPN repository independent of system UI components.

The launcher uses a BoardVPN adaptive icon: a blue shield foreground, branded
background and a dedicated monochrome shield for themed Android icons.

### Stage 7: AndroidVpnRuntime

Status: implemented.

- Add a single coroutine event queue and session-scoped IDs.
- Centralize ownership of TUN, core, tun2socks, retries and jobs.
- Implement deterministic graceful shutdown with timeout and forced cleanup.

### Stage 8: BoardProxy AAR

Status: implemented.

- Add a reproducible AAR build/import process and required Android ABIs.
- Hide gomobile behind `BoardProxyClient` and `AarBoardProxyClient`.
- Map native callbacks into typed runtime events.
- Route socket protection through `VpnService.protect(fd)` and fail safely when
  protection is rejected.
- Build for `arm64-v8a`, `armeabi-v7a` and `x86_64`, with 16 KiB ELF segment
  alignment for current Android devices.
- Keep the imported artifact at `app/libs/boardproxy.aar`; all generated Java
  types remain behind `infrastructure/core`.

### Stage 9: TUN interface

Status: implemented, including the infrastructure boundary for per-application
split tunneling.

- Add explicit address, route, DNS and MTU configuration.
- Establish the interface through `VpnService.Builder`.
- Make TUN ownership and closure unambiguous on every failure path.
- Persist `AppRoutingPolicy` in settings and read a fresh snapshot immediately
  before each TUN is established.
- Keep the selected application mode and package set independently from the
  `allProxy` flag. With `allProxy=true`, `VpnService.Builder` receives no
  allowed/disallowed application calls, while the user's previous selection is
  retained for later reuse.
- Support `OnlySelectedApps` and `ExcludeSelectedApps` when `allProxy=false`.
  These modes require a non-empty set of valid Android package names; an
  uninstalled package fails tunnel creation with an explicit error.
- Apply application routing only to a new interface. Updating the stored policy
  does not mutate a running VPN. The active session retains the policy used to
  establish its TUN; when it differs from settings, the routing screen offers
  an explicit proxy restart that rebuilds the interface with the new policy.
- Reserve an explicit disabled UDP mode in the tun engine configuration.

The technical API is deliberately direct and does not add a one-method use-case
class:

```kotlin
settingsRepository.setAppRoutingPolicy(
    AppRoutingPolicy(
        mode = AppRoutingMode.ExcludeSelectedApps,
        packageNames = setOf("com.example.direct"),
        allProxy = false,
    )
)
```

A future settings screen only needs to obtain installed package names from
Android, build this domain value and call the repository. It must present allow
and exclude as separate modes because `VpnService.Builder` does not permit both
lists on one interface.

### Stage 10: TCP/UDP tun2socks

Status: implemented with `hev-socks5-tunnel` pinned at upstream commit
`d1b6c6f1a02e2010dbed795d8a40fdc4155e49b2` (MIT), immediately after 2.16.0.

- Select and license-check an implementation with required ABIs.
- Start tun2socks only after the BoardProxy SOCKS endpoint is ready.
- Stop tun2socks before core shutdown.
- Publish `Connected` only after both subsystems are ready.
- Watch the native worker and fail the session if it terminates unexpectedly.
- Use local mapped DNS and standard SOCKS5 UDP relay mode. BoardProxy core
  preserves datagram boundaries and owns the server-side UDP socket.

### Stage 11: reconnect lifecycle

- Status: implemented for core session loss and Android physical-network/DNS
  changes.

- Keep TUN stable during recoverable BoardProxy reconnects.
- Add capped exponential backoff with jitter.
- Cancel all retries immediately on manual disconnect.
- Distinguish recoverable network failures from configuration failures.
- Limit full runtime restarts after TUN or tun2socks failure.

`UnderlyingNetworkRoute` observes both the selected non-VPN network and its
`LinkProperties`. A physical network switch, loss or DNS-address change sends a
serialized runtime message. The mobile facade calls `Client.Reconnect()`, which
publishes `reconnecting` and closes only the current mux session. The existing
core reconnect loop creates freshly protected/bound control sockets while the
local SOCKS listener, TUN descriptor and packet forwarder remain alive.

### Stage 12: profile persistence

Status: implemented. Clipboard import and durable unencrypted JSON persistence
work; Android Keystore protection is still open.

- Persist profiles and selected profile through a domain repository.
- Keep the storage envelope, serializers and file layout in infrastructure.
- Evaluate Android Keystore before storing sensitive material.
- Never persist runtime session state.

Clipboard import validates the same 64-byte base64url key material accepted by
the Go keylink parser, derives a stable non-secret profile ID, uses the optional
keylink label as the display name, and never logs or renders the full key.

User preferences live in a second JSON DataStore file, `app_settings.json`,
behind `AppSettingsRepository`: theme mode, connect-on-launch and
the application routing policy. Theme changes apply immediately because
`MainActivity` collects the settings and feeds `BoardVPNTheme`; routing changes
apply on the next VPN connection.

`DataStoreVpnProfileRepository` stores one JSON snapshot,
`filesDir/datastore/vpn_profiles.json`, holding the profile list and the
selected `VpnProfileId`. Domain models are serialized directly: `VpnProfile` and
`VpnProfileId` are `@Serializable`, and `BoardProxyKeylink` uses a string
serializer, so `fromRaw` validation runs again on every read. Separate
persistence DTOs are therefore intentionally absent; only `StoredProfiles`, the
file envelope, lives in `infrastructure/persistence`. Unreadable or invalid
files are reported as `CorruptionException` and replaced with an empty snapshot
instead of crashing the application. Nothing is encrypted at this stage, so the
key material is only protected by app-private storage.

### Stage 13: QR scanner

- Add CameraX preview and ML Kit barcode recognition.
- Parse, validate and save a profile without auto-starting VPN.
- Throttle duplicate scan results and show validation errors in UI.

### Stage 14: diagnostics

- Add structured logging categories for UI, VPN, core, TUN and profiles.
- Include the session ID in every runtime log.
- Log transitions, reconnect, graceful shutdown and forced cleanup.
- Redact keylinks and secrets.

### Stage 15: verification

- Unit-test reducer, retry policy, parser, repositories and ViewModels.
- Integration-test runtime races using fake core and TUN implementations.
- Test grant/deny/revoke, repeated commands, network switching, app background,
  process death, notification disconnect and long-running operation.
- Test API 26, a current API level, ARM64 hardware and x86_64 emulator when the
  AAR provides that ABI.

### Stage 16: optional Gradle modularization

Keep package boundaries while the application is small. Split packages into
`:domain`, `:infrastructure:*` and `:ui:*` modules only after build time, team
ownership, reuse or boundary enforcement provides a concrete benefit.

## Stage completion rule

A stage is complete only when its code compiles, relevant automated tests pass,
failure and cleanup paths are covered, and no dependency direction described in
this document is violated.
