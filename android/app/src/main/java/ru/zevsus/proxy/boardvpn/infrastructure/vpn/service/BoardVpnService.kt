package ru.zevsus.proxy.boardvpn.infrastructure.vpn.service

import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.VpnService
import android.os.Build
import android.os.Handler
import android.os.Looper
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import ru.zevsus.proxy.boardvpn.app.BoardVpnApplication
import ru.zevsus.proxy.boardvpn.domain.model.VpnConnectResult
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionPhase
import ru.zevsus.proxy.boardvpn.domain.repository.VpnRepository
import ru.zevsus.proxy.boardvpn.infrastructure.core.SocketProtector
import ru.zevsus.proxy.boardvpn.infrastructure.tun.HevPacketForwarder
import ru.zevsus.proxy.boardvpn.infrastructure.tun.VpnTunnelConfig
import ru.zevsus.proxy.boardvpn.infrastructure.tun.VpnTunnelFactory
import ru.zevsus.proxy.boardvpn.infrastructure.vpn.AndroidVpnRuntime
import ru.zevsus.proxy.boardvpn.infrastructure.vpn.UnderlyingNetworkRoute

class BoardVpnService : VpnService() {
    private val serviceScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val mainHandler = Handler(Looper.getMainLooper())

    private lateinit var notifications: VpnNotificationManager
    private lateinit var runtime: AndroidVpnRuntime
    private lateinit var underlyingRoute: UnderlyingNetworkRoute
    private lateinit var vpnRepository: VpnRepository

    private var serviceState: ServiceState = ServiceState.Idle

    override fun onCreate() {
        super.onCreate()
        val container = (application as BoardVpnApplication).container
        vpnRepository = container.vpnRepository
        underlyingRoute = UnderlyingNetworkRoute(this) { change ->
            if (::runtime.isInitialized) {
                runtime.underlyingNetworkChanged(change)
            }
        }
        notifications = VpnNotificationManager(this).also { it.createChannel() }
        runtime = AndroidVpnRuntime(
            scope = serviceScope,
            profiles = container.profileRepository,
            subscriptionSyncManager = container.subscriptionSyncManager,
            repository = container.androidVpnRepository,
            clientFactory = container.boardProxyClientFactory,
            tunnelFactory = VpnTunnelFactory(this) {
                VpnTunnelConfig(
                    appRoutingPolicy = container.settingsRepository
                        .observeSettings()
                        .first()
                        .appRoutingPolicy,
                )
            },
            packetForwarder = HevPacketForwarder(this),
            socketProtector = object : SocketProtector {
                override fun protect(fileDescriptor: Int): Boolean =
                    underlyingRoute.protectAndBind(this@BoardVpnService, fileDescriptor)

                override fun dnsAddress(): String = underlyingRoute.dnsAddress()
            },
            onPhaseChanged = notifications::update,
            onStatisticsChanged = notifications::updateStatistics,
            onStopped = ::stopServiceAfterRuntime,
        )
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (val command = VpnServiceCommand.fromIntent(intent)) {
            is VpnServiceCommand.Connect -> {
                serviceState = ServiceState.Running(command.profileId)
                startInForeground(VpnSessionPhase.Starting)
                runtime.start(command.sessionId, command.profileId)
            }
            VpnServiceCommand.Pause -> pauseRuntime()
            VpnServiceCommand.Resume -> resumeRuntime()
            VpnServiceCommand.Restart -> restartRuntime()
            VpnServiceCommand.Disconnect -> stopRuntime(keepNotification = false)
            null -> stopSelf(startId)
        }
        return START_NOT_STICKY
    }

    override fun onRevoke() {
        runtime.permissionRevoked()
    }

    override fun onDestroy() {
        runtime.closeImmediately()
        underlyingRoute.close()
        serviceScope.cancel()
        super.onDestroy()
    }

    private fun startInForeground(phase: VpnSessionPhase) {
        val notification = notifications.build(phase)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE &&
            applicationInfo.targetSdkVersion >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE
        ) {
            startForeground(
                VpnNotificationManager.NOTIFICATION_ID,
                notification,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE,
            )
        } else {
            startForeground(VpnNotificationManager.NOTIFICATION_ID, notification)
        }
    }

    private fun stopServiceAfterRuntime() {
        mainHandler.post {
            when (val state = serviceState) {
                is ServiceState.Stopping -> {
                    if (state.restartAfterStop) {
                        resumeProfile(state.profileId)
                    } else if (state.keepNotification) {
                        serviceState = ServiceState.Paused(state.profileId)
                        notifications.showPaused()
                    } else {
                        finishService()
                    }
                }
                ServiceState.Idle,
                is ServiceState.Paused,
                is ServiceState.Resuming,
                is ServiceState.Running,
                -> finishService()
            }
        }
    }

    private fun pauseRuntime() {
        if (serviceState !is ServiceState.Running) return
        stopRuntime(keepNotification = true)
    }

    private fun resumeRuntime() {
        val pausedState = serviceState as? ServiceState.Paused ?: return
        resumeProfile(pausedState.profileId)
    }

    private fun restartRuntime() {
        if (serviceState !is ServiceState.Running) return
        stopRuntime(keepNotification = true, restartAfterStop = true)
    }

    private fun resumeProfile(profileId: VpnProfileId) {
        serviceState = ServiceState.Resuming(profileId)
        notifications.update(VpnSessionPhase.Starting)

        serviceScope.launch {
            when (vpnRepository.connect(profileId)) {
                VpnConnectResult.Started,
                VpnConnectResult.AlreadyRunning,
                -> Unit
                is VpnConnectResult.Failed,
                is VpnConnectResult.ProfileNotFound,
                -> mainHandler.post(::finishService)
            }
        }
    }

    private fun stopRuntime(
        keepNotification: Boolean,
        restartAfterStop: Boolean = false,
    ) {
        val profileId = serviceState.profileId ?: run {
            finishService()
            return
        }
        serviceState = ServiceState.Stopping(profileId, keepNotification, restartAfterStop)
        notifications.update(VpnSessionPhase.Stopping)
        runtime.stop()
    }

    private fun finishService() {
        serviceState = ServiceState.Idle
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    private sealed interface ServiceState {
        val profileId: VpnProfileId?

        data object Idle : ServiceState {
            override val profileId: VpnProfileId? = null
        }

        data class Running(
            override val profileId: VpnProfileId,
        ) : ServiceState

        data class Stopping(
            override val profileId: VpnProfileId,
            val keepNotification: Boolean,
            val restartAfterStop: Boolean,
        ) : ServiceState

        data class Paused(
            override val profileId: VpnProfileId,
        ) : ServiceState

        data class Resuming(
            override val profileId: VpnProfileId,
        ) : ServiceState
    }
}
