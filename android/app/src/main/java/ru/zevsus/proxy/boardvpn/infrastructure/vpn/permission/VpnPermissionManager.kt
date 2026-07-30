package ru.zevsus.proxy.boardvpn.infrastructure.vpn.permission

import android.content.Context
import android.content.Intent
import android.net.VpnService

sealed interface VpnPermissionStatus {
    data object Granted : VpnPermissionStatus

    data class ConsentRequired(
        val intent: Intent,
    ) : VpnPermissionStatus
}

class VpnPermissionManager(
    private val context: Context,
) {
    fun prepare(): VpnPermissionStatus {
        val consentIntent = VpnService.prepare(context)
        return if (consentIntent == null) {
            VpnPermissionStatus.Granted
        } else {
            VpnPermissionStatus.ConsentRequired(consentIntent)
        }
    }
}
