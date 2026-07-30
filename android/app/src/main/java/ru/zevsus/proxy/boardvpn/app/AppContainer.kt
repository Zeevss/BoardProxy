package ru.zevsus.proxy.boardvpn.app

import android.content.Context
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import ru.zevsus.proxy.boardvpn.domain.repository.AppSettingsRepository
import ru.zevsus.proxy.boardvpn.domain.repository.VpnProfileRepository
import ru.zevsus.proxy.boardvpn.domain.repository.VpnRepository
import ru.zevsus.proxy.boardvpn.infrastructure.core.AarBoardProxyClientFactory
import ru.zevsus.proxy.boardvpn.infrastructure.core.BoardProxyClientFactory
import ru.zevsus.proxy.boardvpn.infrastructure.persistence.DataStoreAppSettingsRepository
import ru.zevsus.proxy.boardvpn.infrastructure.persistence.DataStoreVpnProfileRepository
import ru.zevsus.proxy.boardvpn.infrastructure.profile.ClipboardKeylinkReader
import ru.zevsus.proxy.boardvpn.infrastructure.vpn.AndroidVpnRepository
import ru.zevsus.proxy.boardvpn.infrastructure.vpn.permission.VpnPermissionManager

/**
 * Composition root for application-scoped dependencies.
 *
 * Concrete domain and infrastructure dependencies will be registered here as
 * they are introduced. Keeping construction in one place prevents Android and
 * implementation details from leaking into the UI layer.
 */
class AppContainer(
    val applicationContext: Context,
) {
    private val storageScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    val profileRepository: VpnProfileRepository = DataStoreVpnProfileRepository.create(
        context = applicationContext,
        scope = storageScope,
    )

    val settingsRepository: AppSettingsRepository = DataStoreAppSettingsRepository.create(
        context = applicationContext,
        scope = storageScope,
    )

    val androidVpnRepository = AndroidVpnRepository(
        context = applicationContext,
        profiles = profileRepository,
    )

    val vpnRepository: VpnRepository = androidVpnRepository

    val boardProxyClientFactory: BoardProxyClientFactory = AarBoardProxyClientFactory()

    val vpnPermissionManager = VpnPermissionManager(applicationContext)

    val clipboardKeylinkReader = ClipboardKeylinkReader(applicationContext)
}
