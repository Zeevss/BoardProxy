package ru.zevsus.proxy.boardvpn.ui.home

import kotlin.time.Duration
import ru.zevsus.proxy.boardvpn.domain.model.VpnFailure
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnStatistics

enum class HomeConnectionStatus {
    Disconnected,
    Connecting,
    Connected,
    Reconnecting,
    Disconnecting,
}

sealed interface HomeProblem {
    data object ProfileNotFound : HomeProblem
    data object PermissionDenied : HomeProblem

    data class SessionFailed(
        val failure: VpnFailure,
    ) : HomeProblem
}

data class HomeUiState(
    val status: HomeConnectionStatus = HomeConnectionStatus.Disconnected,
    val profiles: List<VpnProfile> = emptyList(),
    val selectedProfileId: VpnProfileId? = null,
    val statistics: VpnStatistics = VpnStatistics.Empty,
    val connectedDuration: Duration? = null,
    val problem: HomeProblem? = null,
) {
    val selectedProfile: VpnProfile?
        get() = profiles.firstOrNull { it.id == selectedProfileId }

    val canConnect: Boolean
        get() = status == HomeConnectionStatus.Disconnected && selectedProfile != null

    val canDisconnect: Boolean
        get() = status == HomeConnectionStatus.Connecting ||
            status == HomeConnectionStatus.Connected ||
            status == HomeConnectionStatus.Reconnecting

    /** True while traffic counters and the session timer are meaningful. */
    val isSessionLive: Boolean
        get() = status == HomeConnectionStatus.Connected ||
            status == HomeConnectionStatus.Reconnecting
}

sealed interface HomeAction {
    /** Single button: connects when idle, disconnects while a session is live. */
    data object ToggleConnection : HomeAction

    data object DismissProblem : HomeAction

    data class SelectProfile(
        val profileId: VpnProfileId,
    ) : HomeAction
}
