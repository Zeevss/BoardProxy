package ru.zevsus.proxy.boardvpn.app

import android.app.Activity
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.result.contract.ActivityResultContracts
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionState
import ru.zevsus.proxy.boardvpn.infrastructure.vpn.permission.VpnPermissionStatus

/**
 * Invisible launcher-shortcut trampoline. It toggles the proxy without opening
 * the main task; only Android's VPN consent dialog can become visible.
 */
class ProxyToggleActivity : ComponentActivity() {
    private val container: AppContainer
        get() = (application as BoardVpnApplication).container

    private var pendingProfileId: ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId? = null

    private val consentLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        val profileId = pendingProfileId
        pendingProfileId = null
        if (result.resultCode == Activity.RESULT_OK && profileId != null) {
            lifecycleScope.launch {
                container.vpnRepository.connect(profileId)
                finish()
            }
        } else {
            finish()
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        lifecycleScope.launch { toggleProxy() }
    }

    private suspend fun toggleProxy() {
        when (container.vpnRepository.observeSession().first()) {
            is VpnSessionState.Active -> {
                container.vpnRepository.disconnect()
                finish()
            }
            VpnSessionState.Idle,
            is VpnSessionState.Failed,
            -> connect()
        }
    }

    private suspend fun connect() {
        val profileId = container.profileRepository.observeSelectedProfileId().first()
            ?: container.profileRepository.observeProfiles().first().firstOrNull()?.id
            ?: run {
                finish()
                return
            }

        when (val permission = container.vpnPermissionManager.prepare()) {
            VpnPermissionStatus.Granted -> {
                container.vpnRepository.connect(profileId)
                finish()
            }
            is VpnPermissionStatus.ConsentRequired -> {
                pendingProfileId = profileId
                consentLauncher.launch(permission.intent)
            }
        }
    }
}
