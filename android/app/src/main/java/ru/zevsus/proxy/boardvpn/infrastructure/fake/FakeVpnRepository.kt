package ru.zevsus.proxy.boardvpn.infrastructure.fake

import java.util.concurrent.atomic.AtomicLong
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import ru.zevsus.proxy.boardvpn.domain.logic.VpnEvent
import ru.zevsus.proxy.boardvpn.domain.logic.VpnStateReducer
import ru.zevsus.proxy.boardvpn.domain.model.VpnConnectResult
import ru.zevsus.proxy.boardvpn.domain.model.VpnFailure
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionPhase
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionState
import ru.zevsus.proxy.boardvpn.domain.model.VpnStatistics
import ru.zevsus.proxy.boardvpn.domain.repository.VpnProfileRepository
import ru.zevsus.proxy.boardvpn.domain.repository.VpnRepository

data class FakeVpnTiming(
    val startupStepMillis: Long = 300,
    val reconnectMillis: Long = 750,
    val statisticsTickMillis: Long = 1_000,
)

class FakeVpnRepository(
    private val profiles: VpnProfileRepository,
    private val scope: CoroutineScope,
    private val timing: FakeVpnTiming = FakeVpnTiming(),
) : VpnRepository {
    private val mutex = Mutex()
    private val sessionIds = AtomicLong()
    private val session = MutableStateFlow<VpnSessionState>(VpnSessionState.Idle)
    private val statistics = MutableStateFlow(VpnStatistics.Empty)

    private var runtimeJob: Job? = null

    override fun observeSession(): Flow<VpnSessionState> = session.asStateFlow()

    override fun observeStatistics(): Flow<VpnStatistics> = statistics.asStateFlow()

    override suspend fun connect(profileId: VpnProfileId): VpnConnectResult {
        if (profiles.getProfile(profileId) == null) {
            return VpnConnectResult.ProfileNotFound(profileId)
        }

        return mutex.withLock {
            if (session.value is VpnSessionState.Active) {
                return@withLock VpnConnectResult.AlreadyRunning
            }

            val sessionId = VpnSessionId(sessionIds.incrementAndGet())
            session.value = VpnStateReducer.reduce(
                session.value,
                VpnEvent.ConnectRequested(sessionId, profileId),
            )
            statistics.value = VpnStatistics.Empty

            runtimeJob?.cancel()
            runtimeJob = scope.launch { runSession(sessionId) }
            VpnConnectResult.Started
        }
    }

    override suspend fun disconnect() {
        val shutdown = mutex.withLock {
            val active = session.value as? VpnSessionState.Active ?: return
            session.value = VpnStateReducer.reduce(
                active,
                VpnEvent.DisconnectRequested(active.sessionId),
            )

            Shutdown(active.sessionId, runtimeJob).also { runtimeJob = null }
        }

        shutdown.job?.cancelAndJoin()

        mutex.withLock {
            session.value = VpnStateReducer.reduce(
                session.value,
                VpnEvent.ShutdownCompleted(shutdown.sessionId),
            )
            statistics.value = VpnStatistics.Empty
        }
    }

    suspend fun simulateReconnect(
        failure: VpnFailure = VpnFailure.CoreConnectionLost("Simulated network loss"),
    ): Boolean {
        val sessionId = mutex.withLock {
            val active = session.value as? VpnSessionState.Active ?: return false
            if (active.phase != VpnSessionPhase.Connected) return false

            session.value = VpnStateReducer.reduce(
                active,
                VpnEvent.CoreReconnectStarted(active.sessionId, attempt = 1, reason = failure),
            )
            active.sessionId
        }

        delay(timing.reconnectMillis)

        return mutex.withLock {
            val active = session.value as? VpnSessionState.Active ?: return@withLock false
            if (active.sessionId != sessionId || active.phase !is VpnSessionPhase.Reconnecting) {
                return@withLock false
            }

            session.value = VpnStateReducer.reduce(active, VpnEvent.CoreConnected(sessionId))
            true
        }
    }

    suspend fun simulateFailure(failure: VpnFailure): Boolean {
        val job = mutex.withLock {
            val active = session.value as? VpnSessionState.Active ?: return false
            session.value = VpnStateReducer.reduce(
                active,
                VpnEvent.RuntimeFailed(active.sessionId, failure),
            )
            statistics.value = VpnStatistics.Empty
            runtimeJob.also { runtimeJob = null }
        }

        job?.cancelAndJoin()
        return true
    }

    private suspend fun runSession(sessionId: VpnSessionId) {
        val startupEvents = listOf(
            VpnEvent.TunnelRequested(sessionId),
            VpnEvent.TunnelEstablished(sessionId),
            VpnEvent.CoreConnected(sessionId),
            VpnEvent.TunStarted(sessionId),
        )

        for (event in startupEvents) {
            delay(timing.startupStepMillis)
            if (!applyEvent(event)) return
        }

        while (true) {
            delay(timing.statisticsTickMillis)
            mutex.withLock {
                val active = session.value as? VpnSessionState.Active ?: return
                if (active.sessionId != sessionId) return
                if (active.phase == VpnSessionPhase.Connected) updateStatistics()
            }
        }
    }

    private suspend fun applyEvent(event: VpnEvent): Boolean = mutex.withLock {
        val active = session.value as? VpnSessionState.Active ?: return@withLock false
        if (active.sessionId != event.sessionId) return@withLock false

        session.value = VpnStateReducer.reduce(active, event)
        true
    }

    private fun updateStatistics() {
        val previous = statistics.value
        val uploadRate = 4_096L
        val downloadRate = 16_384L
        statistics.value = previous.copy(
            roundTripTimeMillis = 42,
            uploadedBytes = previous.uploadedBytes + uploadRate,
            downloadedBytes = previous.downloadedBytes + downloadRate,
            uploadBytesPerSecond = uploadRate,
            downloadBytesPerSecond = downloadRate,
        )
    }

    private data class Shutdown(
        val sessionId: VpnSessionId,
        val job: Job?,
    )
}
