package ru.zevsus.proxy.boardvpn.infrastructure.vpn

import android.content.Context
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import ru.zevsus.proxy.boardvpn.domain.logic.VpnEvent
import ru.zevsus.proxy.boardvpn.domain.logic.VpnStateReducer
import ru.zevsus.proxy.boardvpn.domain.model.VpnConnectResult
import ru.zevsus.proxy.boardvpn.domain.model.VpnFailure
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionState
import ru.zevsus.proxy.boardvpn.domain.model.VpnStatistics
import ru.zevsus.proxy.boardvpn.domain.repository.VpnProfileRepository
import ru.zevsus.proxy.boardvpn.domain.repository.VpnRepository
import ru.zevsus.proxy.boardvpn.infrastructure.vpn.service.VpnServiceCommand

class AndroidVpnRepository(
    private val context: Context,
    private val profiles: VpnProfileRepository,
) : VpnRepository {
    private val mutex = Mutex()
    private val session = MutableStateFlow<VpnSessionState>(VpnSessionState.Idle)
    private val statistics = MutableStateFlow(VpnStatistics.Empty)
    private var nextSessionId = 0L

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

            val sessionId = VpnSessionId(++nextSessionId)
            session.value = VpnStateReducer.reduce(
                session.value,
                VpnEvent.ConnectRequested(sessionId, profileId),
            )
            statistics.value = VpnStatistics.Empty

            try {
                context.startForegroundService(
                    VpnServiceCommand.connectIntent(context, sessionId, profileId)
                )
                VpnConnectResult.Started
            } catch (error: Throwable) {
                val failure = VpnFailure.Unexpected(error.message)
                session.value = VpnStateReducer.reduce(
                    session.value,
                    VpnEvent.RuntimeFailed(sessionId, failure),
                )
                VpnConnectResult.Failed(failure)
            }
        }
    }

    override suspend fun disconnect() {
        val shouldStop = mutex.withLock {
            val active = session.value as? VpnSessionState.Active ?: return@withLock false
            session.value = VpnStateReducer.reduce(
                active,
                VpnEvent.DisconnectRequested(active.sessionId),
            )
            true
        }

        if (shouldStop) {
            context.startService(VpnServiceCommand.disconnectIntent(context))
        }
    }

    override suspend fun restart() {
        val active = mutex.withLock {
            session.value as? VpnSessionState.Active
        } ?: return

        if (active.phase != ru.zevsus.proxy.boardvpn.domain.model.VpnSessionPhase.Stopping) {
            context.startService(VpnServiceCommand.restartIntent(context))
        }
    }

    suspend fun applyEvent(event: VpnEvent): VpnSessionState = mutex.withLock {
        VpnStateReducer.reduce(session.value, event).also { session.value = it }
    }

    suspend fun updateStatistics(value: VpnStatistics) {
        mutex.withLock { statistics.value = value }
    }

    suspend fun clearStatistics() {
        updateStatistics(VpnStatistics.Empty)
    }

    fun currentSession(): VpnSessionState = session.value
}
