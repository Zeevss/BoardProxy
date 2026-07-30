package ru.zevsus.proxy.boardvpn.infrastructure.vpn

import android.util.Log
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeout
import ru.zevsus.proxy.boardvpn.domain.logic.VpnEvent
import ru.zevsus.proxy.boardvpn.domain.model.VpnFailure
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionPhase
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionState
import ru.zevsus.proxy.boardvpn.domain.repository.VpnProfileRepository
import ru.zevsus.proxy.boardvpn.infrastructure.core.BoardProxyClient
import ru.zevsus.proxy.boardvpn.infrastructure.core.BoardProxyClientFactory
import ru.zevsus.proxy.boardvpn.infrastructure.core.BoardProxyConfig
import ru.zevsus.proxy.boardvpn.infrastructure.core.BoardProxyListener
import ru.zevsus.proxy.boardvpn.infrastructure.core.BoardProxyStatus
import ru.zevsus.proxy.boardvpn.infrastructure.core.SocketProtector
import ru.zevsus.proxy.boardvpn.infrastructure.tun.PacketForwarder
import ru.zevsus.proxy.boardvpn.infrastructure.tun.VpnTunnel
import ru.zevsus.proxy.boardvpn.infrastructure.tun.VpnTunnelFactory

class AndroidVpnRuntime(
    private val scope: CoroutineScope,
    private val profiles: VpnProfileRepository,
    private val repository: AndroidVpnRepository,
    private val clientFactory: BoardProxyClientFactory,
    private val tunnelFactory: VpnTunnelFactory,
    private val packetForwarder: PacketForwarder,
    private val socketProtector: SocketProtector,
    private val onPhaseChanged: (VpnSessionPhase) -> Unit,
    private val onStatisticsChanged: (ru.zevsus.proxy.boardvpn.domain.model.VpnStatistics) -> Unit,
    private val onStopped: () -> Unit,
) {
    private val messages = Channel<Message>(Channel.UNLIMITED)
    private val closed = AtomicBoolean()
    private var resources: Resources? = null

    private val actorJob = scope.launch {
        for (message in messages) {
            when (message) {
                is Message.Start -> handleStart(message)
                Message.Stop -> shutdown(manual = true)
                Message.PermissionRevoked -> handlePermissionRevoked()
                is Message.CoreStatusChanged -> handleCoreStatus(message)
                is Message.CoreMetricsChanged -> handleCoreMetrics(message)
                is Message.CoreTerminated -> handleCoreTerminated(message)
                is Message.ForwarderTerminated -> handleForwarderTerminated(message)
            }
        }
    }

    fun start(sessionId: VpnSessionId, profileId: VpnProfileId) {
        messages.trySend(Message.Start(sessionId, profileId))
    }

    fun stop() {
        messages.trySend(Message.Stop)
    }

    fun permissionRevoked() {
        messages.trySend(Message.PermissionRevoked)
    }

    fun closeImmediately() {
        if (!closed.compareAndSet(false, true)) return
        messages.close()
        resources?.client?.stop()
        packetForwarder.closeImmediately()
        resources?.tunnel?.close()
        resources = null
        actorJob.cancel()
    }

    private suspend fun handleStart(message: Message.Start) {
        if (resources != null) return

        val profile = profiles.getProfile(message.profileId)
        if (profile == null) {
            failWithoutResources(
                message.sessionId,
                VpnFailure.InvalidProfile("Profile ${message.profileId.value} was not found"),
            )
            return
        }

        apply(VpnEvent.TunnelRequested(message.sessionId))
        val tunnel = try {
            tunnelFactory.establish()
        } catch (error: Throwable) {
            failWithoutResources(
                message.sessionId,
                VpnFailure.TunnelEstablishmentFailed(error.message),
            )
            return
        }
        apply(VpnEvent.TunnelEstablished(message.sessionId))

        val termination = CompletableDeferred<Throwable?>()
        try {
            val listener = runtimeListener(message.sessionId)
            val client = clientFactory.create(
                config = BoardProxyConfig(keylink = profile.keylink, enableUdp = true),
                listener = listener,
                socketProtector = socketProtector,
            )
            resources = Resources(
                sessionId = message.sessionId,
                tunnel = tunnel,
                client = client,
                termination = termination,
            )
            client.start()
            startTerminationWatcher(message.sessionId, client, termination)
        } catch (error: Throwable) {
            termination.complete(error)
            if (resources == null) tunnel.close()
            failAndShutdown(
                message.sessionId,
                VpnFailure.CoreStartFailed(error.message),
            )
        }
    }

    private fun runtimeListener(sessionId: VpnSessionId) = object : BoardProxyListener {
        override fun onStatus(status: BoardProxyStatus, message: String?) {
            messages.trySend(Message.CoreStatusChanged(sessionId, status, message))
        }

        override fun onMetrics(statistics: ru.zevsus.proxy.boardvpn.domain.model.VpnStatistics) {
            messages.trySend(Message.CoreMetricsChanged(sessionId, statistics))
        }

        override fun onLog(level: String, message: String) {
            Log.println(logPriority(level), TAG, "session=${sessionId.value} core $message")
        }
    }

    private fun startTerminationWatcher(
        sessionId: VpnSessionId,
        client: BoardProxyClient,
        termination: CompletableDeferred<Throwable?>,
    ) {
        scope.launch {
            val error = runCatching { client.awaitTermination() }.exceptionOrNull()
            termination.complete(error)
            messages.trySend(Message.CoreTerminated(sessionId, error))
        }
    }

    private suspend fun handleCoreStatus(message: Message.CoreStatusChanged) {
        val current = resources?.takeIf { it.sessionId == message.sessionId } ?: return
        when (message.status) {
            BoardProxyStatus.Disconnected -> {
                if (!current.stopping) {
                    failAndShutdown(
                        message.sessionId,
                        VpnFailure.CoreConnectionLost(message.detail),
                    )
                }
            }
            BoardProxyStatus.Connecting -> Unit
            BoardProxyStatus.Connected -> handleCoreConnected(current)
            BoardProxyStatus.Reconnecting -> {
                current.reconnectAttempt += 1
                apply(
                    VpnEvent.CoreReconnectStarted(
                        sessionId = message.sessionId,
                        attempt = current.reconnectAttempt,
                        reason = message.detail?.let(VpnFailure::CoreConnectionLost),
                    )
                )
            }
            BoardProxyStatus.Stopping -> Unit
            BoardProxyStatus.Error -> failAndShutdown(
                message.sessionId,
                VpnFailure.CoreConnectionLost(message.detail),
            )
        }
    }

    private suspend fun handleCoreConnected(current: Resources) {
        current.reconnectAttempt = 0
        apply(VpnEvent.CoreConnected(current.sessionId))

        if (!current.forwarderStarted) {
            try {
                packetForwarder.start(
                    tunFileDescriptor = current.tunnel.fileDescriptor,
                    socksAddress = LOCAL_SOCKS_ADDRESS,
                )
                current.forwarderStarted = true
                startForwarderWatcher(current.sessionId)
                apply(VpnEvent.TunStarted(current.sessionId))
            } catch (error: Throwable) {
                failAndShutdown(
                    current.sessionId,
                    VpnFailure.TunEngineFailed(error.message),
                )
            }
        }
    }

    private suspend fun handleCoreMetrics(message: Message.CoreMetricsChanged) {
        if (resources?.sessionId == message.sessionId) {
            repository.updateStatistics(message.statistics)
            onStatisticsChanged(message.statistics)
        }
    }

    private suspend fun handleCoreTerminated(message: Message.CoreTerminated) {
        val current = resources?.takeIf { it.sessionId == message.sessionId } ?: return
        if (!current.stopping) {
            failAndShutdown(
                message.sessionId,
                VpnFailure.CoreConnectionLost(message.error?.message),
            )
        }
    }

    private fun startForwarderWatcher(sessionId: VpnSessionId) {
        scope.launch {
            val error = runCatching { packetForwarder.awaitTermination() }.exceptionOrNull()
            messages.trySend(Message.ForwarderTerminated(sessionId, error))
        }
    }

    private suspend fun handleForwarderTerminated(message: Message.ForwarderTerminated) {
        val current = resources?.takeIf { it.sessionId == message.sessionId } ?: return
        if (!current.stopping) {
            failAndShutdown(
                message.sessionId,
                VpnFailure.TunEngineFailed(
                    message.error?.message ?: "tun2socks stopped unexpectedly"
                ),
            )
        }
    }

    private suspend fun handlePermissionRevoked() {
        val current = resources
        if (current == null) {
            onStopped()
            return
        }
        repository.applyEvent(
            VpnEvent.RuntimeFailed(
                current.sessionId,
                VpnFailure.PermissionRevoked("VPN permission was revoked by Android"),
            )
        )
        shutdown(manual = false)
    }

    private suspend fun failAndShutdown(sessionId: VpnSessionId, failure: VpnFailure) {
        Log.e(
            TAG,
            "session=${sessionId.value} failed type=${failure::class.simpleName}" +
                failure.technicalMessage?.let { " reason=$it" }.orEmpty(),
        )
        repository.applyEvent(VpnEvent.RuntimeFailed(sessionId, failure))
        shutdown(manual = false)
    }

    private suspend fun failWithoutResources(sessionId: VpnSessionId, failure: VpnFailure) {
        Log.e(
            TAG,
            "session=${sessionId.value} failed type=${failure::class.simpleName}" +
                failure.technicalMessage?.let { " reason=$it" }.orEmpty(),
        )
        repository.applyEvent(VpnEvent.RuntimeFailed(sessionId, failure))
        repository.clearStatistics()
        onStopped()
    }

    private suspend fun shutdown(manual: Boolean) {
        val current = resources
        if (current == null) {
            onStopped()
            return
        }
        if (current.stopping) return
        current.stopping = true

        if (manual) {
            apply(VpnEvent.DisconnectRequested(current.sessionId))
        }

        runCatching {
            if (current.forwarderStarted) packetForwarder.stop()
        }.onFailure { Log.w(TAG, "Packet forwarder stop failed", it) }

        current.client.stop()
        val terminationFailure = runCatching {
            withTimeout(SHUTDOWN_TIMEOUT_MILLIS) { current.termination.await() }
        }.exceptionOrNull()

        if (terminationFailure != null) {
            repository.applyEvent(
                VpnEvent.RuntimeFailed(
                    current.sessionId,
                    VpnFailure.ShutdownTimedOut(terminationFailure.message),
                )
            )
        }

        current.tunnel.close()
        resources = null
        repository.clearStatistics()

        val state = repository.currentSession()
        if (state is VpnSessionState.Active && state.phase == VpnSessionPhase.Stopping) {
            repository.applyEvent(VpnEvent.ShutdownCompleted(current.sessionId))
        }
        onStopped()
    }

    private suspend fun apply(event: VpnEvent) {
        val state = repository.applyEvent(event)
        if (state is VpnSessionState.Active) onPhaseChanged(state.phase)
    }

    private fun logPriority(level: String): Int = when (level.lowercase()) {
        "debug" -> Log.DEBUG
        "warn", "warning" -> Log.WARN
        "error" -> Log.ERROR
        else -> Log.INFO
    }

    private data class Resources(
        val sessionId: VpnSessionId,
        val tunnel: VpnTunnel,
        val client: BoardProxyClient,
        val termination: CompletableDeferred<Throwable?>,
        var forwarderStarted: Boolean = false,
        var reconnectAttempt: Int = 0,
        var stopping: Boolean = false,
    )

    private sealed interface Message {
        data class Start(val sessionId: VpnSessionId, val profileId: VpnProfileId) : Message
        data object Stop : Message
        data object PermissionRevoked : Message

        data class CoreStatusChanged(
            val sessionId: VpnSessionId,
            val status: BoardProxyStatus,
            val detail: String?,
        ) : Message

        data class CoreMetricsChanged(
            val sessionId: VpnSessionId,
            val statistics: ru.zevsus.proxy.boardvpn.domain.model.VpnStatistics,
        ) : Message

        data class CoreTerminated(
            val sessionId: VpnSessionId,
            val error: Throwable?,
        ) : Message

        data class ForwarderTerminated(
            val sessionId: VpnSessionId,
            val error: Throwable?,
        ) : Message
    }

    companion object {
        private const val TAG = "BoardVpnRuntime"
        private const val LOCAL_SOCKS_ADDRESS = "127.0.0.1:1080"
        private const val SHUTDOWN_TIMEOUT_MILLIS = 5_000L
    }
}
