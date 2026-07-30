package ru.zevsus.proxy.boardvpn.infrastructure.vpn.tile

import android.Manifest
import android.annotation.SuppressLint
import android.app.PendingIntent
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.drawable.Icon
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import ru.zevsus.proxy.boardvpn.R
import ru.zevsus.proxy.boardvpn.app.BoardVpnApplication
import ru.zevsus.proxy.boardvpn.app.MainActivity
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionPhase
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionState
import ru.zevsus.proxy.boardvpn.infrastructure.vpn.permission.VpnPermissionStatus

class BoardVpnTileService : TileService() {
    private val serviceScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val container
        get() = (application as BoardVpnApplication).container

    private var listeningJob: Job? = null

    override fun onStartListening() {
        super.onStartListening()
        listeningJob?.cancel()
        listeningJob = serviceScope.launch {
            container.vpnRepository.observeSession().collectLatest(::render)
        }
    }

    override fun onStopListening() {
        listeningJob?.cancel()
        listeningJob = null
        super.onStopListening()
    }

    override fun onClick() {
        super.onClick()
        unlockAndRun {
            serviceScope.launch { handleClick() }
        }
    }

    override fun onDestroy() {
        serviceScope.cancel()
        super.onDestroy()
    }

    private suspend fun handleClick() {
        when (val session = container.vpnRepository.observeSession().first()) {
            is VpnSessionState.Active -> {
                if (session.phase == VpnSessionPhase.Connected) {
                    container.vpnRepository.disconnect()
                }
            }
            VpnSessionState.Idle,
            is VpnSessionState.Failed,
            -> connectOrRequestConsent()
        }
    }

    private suspend fun connectOrRequestConsent() {
        val profileId = container.profileRepository.observeSelectedProfileId().first()
            ?: container.profileRepository.observeProfiles().first().firstOrNull()?.id

        if (profileId == null) {
            openMainActivity(toggleProxy = false)
            return
        }

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED
        ) {
            openMainActivity(toggleProxy = true)
            return
        }

        when (container.vpnPermissionManager.prepare()) {
            VpnPermissionStatus.Granted -> container.vpnRepository.connect(profileId)
            is VpnPermissionStatus.ConsentRequired -> openMainActivity(toggleProxy = true)
        }
    }

    private fun render(session: VpnSessionState) {
        val tile = qsTile ?: return
        val presentation = session.toTilePresentation()
        tile.icon = Icon.createWithResource(this, R.drawable.ic_vpn_shield)
        tile.label = getString(R.string.quick_settings_tile_label)
        tile.state = presentation.state
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            tile.subtitle = getString(presentation.statusText)
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            tile.stateDescription = getString(presentation.statusText)
        }
        tile.contentDescription = getString(
            R.string.quick_settings_tile_description,
            getString(presentation.statusText),
        )
        tile.updateTile()
    }

    @SuppressLint("StartActivityAndCollapseDeprecated")
    private fun openMainActivity(toggleProxy: Boolean) {
        val intent = Intent(this, MainActivity::class.java).apply {
            action = if (toggleProxy) MainActivity.ACTION_TOGGLE_PROXY else Intent.ACTION_MAIN
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
        }

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            val pendingIntent = PendingIntent.getActivity(
                this,
                TILE_ACTIVITY_REQUEST_CODE,
                intent,
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
            )
            startActivityAndCollapse(pendingIntent)
        } else {
            @Suppress("DEPRECATION")
            startActivityAndCollapse(intent)
        }
    }

    companion object {
        private const val TILE_ACTIVITY_REQUEST_CODE = 2001
    }
}

internal data class TilePresentation(
    val state: Int,
    val statusText: Int,
)

internal fun VpnSessionState.toTilePresentation(): TilePresentation = when (this) {
    VpnSessionState.Idle,
    is VpnSessionState.Failed,
    -> TilePresentation(
        state = Tile.STATE_INACTIVE,
        statusText = R.string.home_status_disconnected,
    )
    is VpnSessionState.Active -> when (phase) {
        VpnSessionPhase.Connected -> TilePresentation(
            state = Tile.STATE_ACTIVE,
            statusText = R.string.home_status_connected,
        )
        VpnSessionPhase.Stopping -> TilePresentation(
            state = Tile.STATE_UNAVAILABLE,
            statusText = R.string.home_status_disconnecting,
        )
        VpnSessionPhase.Starting,
        VpnSessionPhase.RequestingTunnel,
        VpnSessionPhase.ConnectingCore,
        VpnSessionPhase.StartingTun,
        is VpnSessionPhase.Reconnecting,
        -> TilePresentation(
            state = Tile.STATE_UNAVAILABLE,
            statusText = if (phase is VpnSessionPhase.Reconnecting) {
                R.string.home_status_reconnecting
            } else {
                R.string.home_status_connecting
            },
        )
    }
}
