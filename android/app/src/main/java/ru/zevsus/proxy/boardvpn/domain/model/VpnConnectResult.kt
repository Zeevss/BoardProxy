package ru.zevsus.proxy.boardvpn.domain.model

sealed interface VpnConnectResult {
    data object Started : VpnConnectResult
    data object AlreadyRunning : VpnConnectResult

    data class Failed(
        val failure: VpnFailure,
    ) : VpnConnectResult

    data class ProfileNotFound(
        val profileId: VpnProfileId,
    ) : VpnConnectResult
}
