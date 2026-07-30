package ru.zevsus.proxy.boardvpn.infrastructure.vpn.service

import android.content.Context
import android.content.Intent
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionId

sealed interface VpnServiceCommand {
    data class Connect(
        val sessionId: VpnSessionId,
        val profileId: VpnProfileId,
    ) : VpnServiceCommand

    data object Pause : VpnServiceCommand
    data object Resume : VpnServiceCommand
    data object Restart : VpnServiceCommand
    data object Disconnect : VpnServiceCommand

    companion object {
        private const val ACTION_CONNECT = "ru.zevsus.proxy.boardvpn.action.CONNECT"
        private const val ACTION_PAUSE = "ru.zevsus.proxy.boardvpn.action.PAUSE"
        private const val ACTION_RESUME = "ru.zevsus.proxy.boardvpn.action.RESUME"
        private const val ACTION_RESTART = "ru.zevsus.proxy.boardvpn.action.RESTART"
        private const val ACTION_DISCONNECT = "ru.zevsus.proxy.boardvpn.action.DISCONNECT"
        private const val EXTRA_SESSION_ID = "session_id"
        private const val EXTRA_PROFILE_ID = "profile_id"

        fun connectIntent(
            context: Context,
            sessionId: VpnSessionId,
            profileId: VpnProfileId,
        ) = Intent(context, BoardVpnService::class.java).apply {
            action = ACTION_CONNECT
            putExtra(EXTRA_SESSION_ID, sessionId.value)
            putExtra(EXTRA_PROFILE_ID, profileId.value)
        }

        fun disconnectIntent(context: Context) =
            Intent(context, BoardVpnService::class.java).apply {
                action = ACTION_DISCONNECT
            }

        fun pauseIntent(context: Context) =
            Intent(context, BoardVpnService::class.java).apply {
                action = ACTION_PAUSE
            }

        fun resumeIntent(context: Context) =
            Intent(context, BoardVpnService::class.java).apply {
                action = ACTION_RESUME
            }

        fun restartIntent(context: Context) =
            Intent(context, BoardVpnService::class.java).apply {
                action = ACTION_RESTART
            }

        fun fromIntent(intent: Intent?): VpnServiceCommand? = when (intent?.action) {
            ACTION_CONNECT -> {
                val sessionId = intent.getLongExtra(EXTRA_SESSION_ID, 0)
                val profileId = intent.getStringExtra(EXTRA_PROFILE_ID)
                if (sessionId > 0 && !profileId.isNullOrBlank()) {
                    Connect(VpnSessionId(sessionId), VpnProfileId(profileId))
                } else {
                    null
                }
            }
            ACTION_PAUSE -> Pause
            ACTION_RESUME -> Resume
            ACTION_RESTART -> Restart
            ACTION_DISCONNECT -> Disconnect
            else -> null
        }
    }
}
