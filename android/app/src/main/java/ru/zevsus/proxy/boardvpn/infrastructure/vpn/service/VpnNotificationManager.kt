package ru.zevsus.proxy.boardvpn.infrastructure.vpn.service

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.graphics.drawable.Icon
import android.os.Build
import java.util.Locale
import ru.zevsus.proxy.boardvpn.R
import ru.zevsus.proxy.boardvpn.app.MainActivity
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionPhase
import ru.zevsus.proxy.boardvpn.domain.model.VpnStatistics

class VpnNotificationManager(
    private val context: Context,
) {
    private val manager = context.getSystemService(NotificationManager::class.java)
    private var currentPhase: VpnSessionPhase = VpnSessionPhase.Starting
    private var currentStatistics: VpnStatistics = VpnStatistics.Empty

    fun createChannel() {
        manager.createNotificationChannel(
            NotificationChannel(
                CHANNEL_ID,
                context.getString(R.string.vpn_notification_channel),
                NotificationManager.IMPORTANCE_LOW,
            )
        )
    }

    fun build(
        phase: VpnSessionPhase,
        statistics: VpnStatistics = currentStatistics,
    ): Notification = buildNotification(
        statusText = context.getString(phase.notificationText()),
        contentText = if (phase == VpnSessionPhase.Connected) {
            context.getString(
                R.string.vpn_notification_speed,
                formatBytesPerSecond(statistics.downloadBytesPerSecond),
                formatBytesPerSecond(statistics.uploadBytesPerSecond),
            )
        } else {
            context.getString(phase.notificationText())
        },
        showStatusAsSubText = phase == VpnSessionPhase.Connected,
        primaryAction = NotificationAction.Pause,
    )

    private fun buildNotification(
        statusText: String,
        contentText: String,
        showStatusAsSubText: Boolean,
        primaryAction: NotificationAction,
    ): Notification {
        val contentIntent = PendingIntent.getActivity(
            context,
            0,
            Intent(context, MainActivity::class.java),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val primaryIntent = PendingIntent.getService(
            context,
            primaryAction.requestCode,
            primaryAction.intent(context),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val finishIntent = PendingIntent.getService(
            context,
            FINISH_REQUEST_CODE,
            VpnServiceCommand.disconnectIntent(context),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )

        val builder = Notification.Builder(context, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_vpn_shield)
            .setContentTitle(context.getString(R.string.app_name))
            .setContentText(contentText)
            .setContentIntent(contentIntent)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setCategory(Notification.CATEGORY_SERVICE)
            .setVisibility(Notification.VISIBILITY_PRIVATE)
            .setShowWhen(false)
            .addAction(
                Notification.Action.Builder(
                    Icon.createWithResource(context, primaryAction.icon),
                    context.getString(primaryAction.label),
                    primaryIntent,
                ).build()
            )
            .addAction(
                Notification.Action.Builder(
                    Icon.createWithResource(context, R.drawable.ic_vpn_power),
                    context.getString(R.string.action_finish),
                    finishIntent,
                ).build()
            )

        if (showStatusAsSubText) builder.setSubText(statusText)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            builder.setForegroundServiceBehavior(Notification.FOREGROUND_SERVICE_IMMEDIATE)
        }
        return builder.build()
    }

    fun update(phase: VpnSessionPhase) {
        currentPhase = phase
        manager.notify(NOTIFICATION_ID, build(phase))
    }

    fun updateStatistics(statistics: VpnStatistics) {
        if (statistics == currentStatistics) return
        currentStatistics = statistics
        if (currentPhase == VpnSessionPhase.Connected) {
            manager.notify(NOTIFICATION_ID, build(currentPhase, statistics))
        }
    }

    fun showPaused() {
        manager.notify(
            NOTIFICATION_ID,
            buildNotification(
                statusText = context.getString(R.string.vpn_notification_paused),
                contentText = context.getString(R.string.vpn_notification_paused),
                showStatusAsSubText = false,
                primaryAction = NotificationAction.Resume,
            )
        )
    }

    private fun VpnSessionPhase.notificationText(): Int = when (this) {
        VpnSessionPhase.Starting,
        VpnSessionPhase.RequestingTunnel,
        VpnSessionPhase.ConnectingCore,
        VpnSessionPhase.StartingTun,
        -> R.string.home_status_connecting
        VpnSessionPhase.Connected -> R.string.home_status_connected
        is VpnSessionPhase.Reconnecting -> R.string.home_status_reconnecting
        VpnSessionPhase.Stopping -> R.string.home_status_disconnecting
    }

    companion object {
        const val NOTIFICATION_ID = 1001
        private const val CHANNEL_ID = "vpn_connection"
        private const val FINISH_REQUEST_CODE = 3
    }

    private enum class NotificationAction(
        val requestCode: Int,
        val label: Int,
        val icon: Int,
        val intent: (Context) -> Intent,
    ) {
        Pause(
            requestCode = 1,
            label = R.string.action_pause,
            icon = R.drawable.ic_vpn_pause,
            intent = { VpnServiceCommand.pauseIntent(it) },
        ),
        Resume(
            requestCode = 2,
            label = R.string.action_resume,
            icon = R.drawable.ic_vpn_play,
            intent = { VpnServiceCommand.resumeIntent(it) },
        ),
    }
}

private fun formatBytesPerSecond(bytesPerSecond: Long): String {
    val safeValue = bytesPerSecond.coerceAtLeast(0)
    return when {
        safeValue >= GIGABYTE -> "%.1f GB/s".formatUs(safeValue / GIGABYTE)
        safeValue >= MEGABYTE -> "%.1f MB/s".formatUs(safeValue / MEGABYTE)
        safeValue >= KILOBYTE -> "%.0f KB/s".formatUs(safeValue / KILOBYTE)
        else -> "$safeValue B/s"
    }
}

private fun String.formatUs(value: Double): String = String.format(Locale.US, this, value)

private const val KILOBYTE = 1_024.0
private const val MEGABYTE = 1_024.0 * 1_024.0
private const val GIGABYTE = 1_024.0 * 1_024.0 * 1_024.0
