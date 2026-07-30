package ru.zevsus.proxy.boardvpn.app

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import ru.zevsus.proxy.boardvpn.domain.model.AppSettings
import ru.zevsus.proxy.boardvpn.domain.model.VpnSessionState
import ru.zevsus.proxy.boardvpn.infrastructure.vpn.permission.VpnPermissionStatus
import ru.zevsus.proxy.boardvpn.ui.home.HomeViewModel
import ru.zevsus.proxy.boardvpn.ui.navigation.BoardVpnApp
import ru.zevsus.proxy.boardvpn.ui.profiles.ProfilesViewModel
import ru.zevsus.proxy.boardvpn.ui.settings.SettingsViewModel
import ru.zevsus.proxy.boardvpn.ui.theme.BoardVPNTheme

class MainActivity : ComponentActivity() {
    private val container: AppContainer
        get() = (application as BoardVpnApplication).container

    private val homeViewModel: HomeViewModel by viewModels {
        viewModelFactory {
            initializer {
                HomeViewModel(
                    vpnRepository = container.vpnRepository,
                    profileRepository = container.profileRepository,
                )
            }
        }
    }

    private val profilesViewModel: ProfilesViewModel by viewModels {
        viewModelFactory {
            initializer { ProfilesViewModel(profileRepository = container.profileRepository) }
        }
    }

    private val settingsViewModel: SettingsViewModel by viewModels {
        viewModelFactory {
            initializer { SettingsViewModel(settingsRepository = container.settingsRepository) }
        }
    }

    private val vpnConsentLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        if (result.resultCode == Activity.RESULT_OK) {
            homeViewModel.connect()
        } else {
            homeViewModel.onVpnPermissionDenied()
        }
    }

    private var connectAfterNotificationPermission = false

    private val notificationPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) {
        if (connectAfterNotificationPermission) {
            connectAfterNotificationPermission = false
            requestVpnConsentAndConnect()
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            val settings by container.settingsRepository
                .observeSettings()
                .collectAsStateWithLifecycle(AppSettings.Default)

            BoardVPNTheme(
                themeMode = settings.themeMode,
                dynamicColor = settings.dynamicColor,
            ) {
                BoardVpnApp(
                    homeViewModel = homeViewModel,
                    profilesViewModel = profilesViewModel,
                    settingsViewModel = settingsViewModel,
                    onConnectRequest = ::requestVpnConnection,
                    onClipboardImportRequest = ::importKeylinkFromClipboard,
                    onOpenSystemVpnSettings = ::openSystemVpnSettings,
                    appVersion = appVersion(),
                )
            }
        }

        if (savedInstanceState == null) {
            if (intent.action == ACTION_TOGGLE_PROXY) {
                toggleProxyFromExternalEntryPoint()
            } else {
                autoConnectIfEnabled()
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        if (intent.action == ACTION_TOGGLE_PROXY) {
            toggleProxyFromExternalEntryPoint()
        }
    }

    private fun requestVpnConnection() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED
        ) {
            connectAfterNotificationPermission = true
            notificationPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
            return
        }
        requestVpnConsentAndConnect()
    }

    private fun requestVpnConsentAndConnect() {
        when (val permission = container.vpnPermissionManager.prepare()) {
            VpnPermissionStatus.Granted -> homeViewModel.connect()
            is VpnPermissionStatus.ConsentRequired -> vpnConsentLauncher.launch(permission.intent)
        }
    }

    private fun importKeylinkFromClipboard() {
        profilesViewModel.importKeylink(container.clipboardKeylinkReader.readText())
    }

    private fun openSystemVpnSettings() {
        runCatching { startActivity(Intent(Settings.ACTION_VPN_SETTINGS)) }
    }

    private fun toggleProxyFromExternalEntryPoint() {
        lifecycleScope.launch {
            when (container.vpnRepository.observeSession().first()) {
                is VpnSessionState.Active -> container.vpnRepository.disconnect()
                VpnSessionState.Idle,
                is VpnSessionState.Failed,
                -> requestVpnConnection()
            }
        }
    }

    /** Starts the stored profile on a cold launch when the user asked for it. */
    private fun autoConnectIfEnabled() {
        lifecycleScope.launch {
            val settings = container.settingsRepository.observeSettings().first()
            if (!settings.autoConnectOnLaunch) return@launch
            if (container.vpnRepository.observeSession().first() != VpnSessionState.Idle) return@launch
            if (container.profileRepository.observeProfiles().first().isEmpty()) return@launch

            requestVpnConnection()
        }
    }

    private fun appVersion(): String =
        packageManager.getPackageInfo(packageName, 0).versionName.orEmpty()

    companion object {
        const val ACTION_TOGGLE_PROXY = "ru.zevsus.proxy.boardvpn.action.TOGGLE_PROXY"
    }
}
