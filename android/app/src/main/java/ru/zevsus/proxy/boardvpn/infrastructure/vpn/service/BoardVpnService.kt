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
import kotlinx.coroutines.launch
import ru.zevsus.proxy.boardvpn.app.BoardVpnApplication
import ru.zevsus.proxy.boardvpn.domain.model.VpnConnectResult
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionPhase
import ru.zevsus.proxy.boardvpn.domain.repository.VpnRepository
import ru.zevsus.proxy.boardvpn.infrastructure.core.SocketProtector
import ru.zevsus.proxy.boardvpn.infrastructure.tun.HevPacketForwarder
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

    private var activeProfileId: VpnProfileId? = null
    private var stopDisposition = StopDisposition.Finish
    private var paused = false
    private var shutdownInProgress = false

    override fun onCreate() {
        super.onCreate()
        val container = (application as BoardVpnApplication).container
        vpnRepository = container.vpnRepository
        underlyingRoute = UnderlyingNetworkRoute(this)
        notifications = VpnNotificationManager(this).also { it.createChannel() }
        runtime = AndroidVpnRuntime(
            scope = serviceScope,
            profiles = container.profileRepository,
            repository = container.androidVpnRepository,
            clientFactory = container.boardProxyClientFactory,
            tunnelFactory = VpnTunnelFactory(this),
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
                activeProfileId = command.profileId
                stopDisposition = StopDisposition.Finish
                paused = false
                shutdownInProgress = false
                startInForeground(VpnSessionPhase.Starting)
                runtime.start(command.sessionId, command.profileId)
            }
            VpnServiceCommand.Pause -> pauseRuntime()
            VpnServiceCommand.Resume -> resumeRuntime()
            VpnServiceCommand.Disconnect -> {
                stopDisposition = StopDisposition.Finish
                paused = false
                shutdownInProgress = true
                notifications.update(VpnSessionPhase.Stopping)
                runtime.stop()
            }
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
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
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
            shutdownInProgress = false
            if (stopDisposition == StopDisposition.Pause && activeProfileId != null) {
                stopDisposition = StopDisposition.Finish
                paused = true
                notifications.showPaused()
            } else {
                finishService()
            }
        }
    }

    private fun pauseRuntime() {
        if (paused || shutdownInProgress || activeProfileId == null) return
        stopDisposition = StopDisposition.Pause
        shutdownInProgress = true
        notifications.update(VpnSessionPhase.Stopping)
        runtime.stop()
    }

    private fun resumeRuntime() {
        if (!paused || shutdownInProgress) return
        val profileId = activeProfileId ?: return
        paused = false
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

    private fun finishService() {
        paused = false
        shutdownInProgress = false
        activeProfileId = null
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    private enum class StopDisposition {
        Pause,
        Finish,
    }
}
