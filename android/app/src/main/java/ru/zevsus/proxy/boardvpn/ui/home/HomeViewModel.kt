package ru.zevsus.proxy.boardvpn.ui.home

import android.os.SystemClock
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlin.time.Duration
import kotlin.time.Duration.Companion.seconds
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import ru.zevsus.proxy.boardvpn.domain.model.VpnConnectResult
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionPhase
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionState
import ru.zevsus.proxy.boardvpn.domain.model.VpnStatistics
import ru.zevsus.proxy.boardvpn.domain.repository.VpnProfileRepository
import ru.zevsus.proxy.boardvpn.domain.repository.VpnRepository

@OptIn(ExperimentalCoroutinesApi::class)
class HomeViewModel(
    private val vpnRepository: VpnRepository,
    private val profileRepository: VpnProfileRepository,
    private val elapsedRealtimeMillis: () -> Long = SystemClock::elapsedRealtime,
) : ViewModel() {
    private val commandProblem = MutableStateFlow<HomeProblem?>(null)

    private val sourceState = combine(
        vpnRepository.observeSession(),
        vpnRepository.observeStatistics(),
        profileRepository.observeProfiles(),
        profileRepository.observeSelectedProfileId(),
    ) { session, statistics, profiles, selectedProfileId ->
        SourceState(session, statistics, profiles, selectedProfileId)
    }

    /** Ticks once per second while a session carries traffic, `null` otherwise. */
    private val connectedDuration: Flow<Duration?> = vpnRepository.observeSession()
        .map { session ->
            (session as? VpnSessionState.Active)
                ?.takeIf { it.phase.isSessionLive() }
                ?.connectedAtElapsedRealtimeMillis
        }
        .distinctUntilChanged()
        .flatMapLatest { connectedAt ->
            if (connectedAt == null) {
                flowOf(null)
            } else {
                flow {
                    while (true) {
                        emit(
                            ((elapsedRealtimeMillis() - connectedAt)
                                .coerceAtLeast(0)
                                / 1_000)
                                .seconds
                        )
                        delay(1.seconds)
                    }
                }
            }
        }

    val uiState = combine(
        sourceState,
        connectedDuration,
        commandProblem,
    ) { source, duration, commandProblem ->
        HomeUiState(
            status = source.session.toHomeStatus(),
            profiles = source.profiles,
            selectedProfileId = source.selectedProfileId ?: source.profiles.firstOrNull()?.id,
            statistics = source.statistics,
            connectedDuration = duration,
            problem = commandProblem ?: source.session.toHomeProblem(),
        )
    }.stateIn(
        scope = viewModelScope,
        started = SharingStarted.WhileSubscribed(5_000),
        initialValue = HomeUiState(),
    )

    fun onAction(action: HomeAction) {
        when (action) {
            HomeAction.ToggleConnection -> toggleConnection()
            HomeAction.DismissProblem -> commandProblem.value = null
            is HomeAction.SelectProfile -> selectProfile(action.profileId)
        }
    }

    /** Called by the hosting Activity once VPN consent is granted. */
    fun connect() {
        commandProblem.value = null

        viewModelScope.launch {
            // The screen may not be collecting uiState yet, for example on an
            // automatic launch connect, so fall back to the stored selection.
            val profileId = uiState.value.selectedProfileId
                ?: profileRepository.observeSelectedProfileId().first()
                ?: profileRepository.observeProfiles().first().firstOrNull()?.id
                ?: return@launch

            when (val result = vpnRepository.connect(profileId)) {
                VpnConnectResult.Started,
                VpnConnectResult.AlreadyRunning,
                -> Unit
                is VpnConnectResult.Failed -> {
                    commandProblem.value = HomeProblem.SessionFailed(result.failure)
                }
                is VpnConnectResult.ProfileNotFound -> {
                    commandProblem.value = HomeProblem.ProfileNotFound
                }
            }
        }
    }

    fun onVpnPermissionDenied() {
        commandProblem.value = HomeProblem.PermissionDenied
    }

    private fun toggleConnection() {
        if (uiState.value.canDisconnect) disconnect()
    }

    private fun selectProfile(profileId: VpnProfileId) {
        viewModelScope.launch { profileRepository.selectProfile(profileId) }
    }

    private fun disconnect() {
        commandProblem.value = null
        viewModelScope.launch { vpnRepository.disconnect() }
    }

    private data class SourceState(
        val session: VpnSessionState,
        val statistics: VpnStatistics,
        val profiles: List<VpnProfile>,
        val selectedProfileId: VpnProfileId?,
    )
}

private fun VpnSessionPhase.isSessionLive(): Boolean =
    this == VpnSessionPhase.Connected || this is VpnSessionPhase.Reconnecting

private fun VpnSessionState.toHomeStatus(): HomeConnectionStatus = when (this) {
    VpnSessionState.Idle,
    is VpnSessionState.Failed,
    -> HomeConnectionStatus.Disconnected
    is VpnSessionState.Active -> when (phase) {
        VpnSessionPhase.Starting,
        VpnSessionPhase.RequestingTunnel,
        VpnSessionPhase.ConnectingCore,
        VpnSessionPhase.StartingTun,
        -> HomeConnectionStatus.Connecting
        VpnSessionPhase.Connected -> HomeConnectionStatus.Connected
        is VpnSessionPhase.Reconnecting -> HomeConnectionStatus.Reconnecting
        VpnSessionPhase.Stopping -> HomeConnectionStatus.Disconnecting
    }
}

private fun VpnSessionState.toHomeProblem(): HomeProblem? = when (this) {
    is VpnSessionState.Failed -> HomeProblem.SessionFailed(failure)
    else -> null
}
