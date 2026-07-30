package ru.zevsus.proxy.boardvpn.domain.model

import kotlinx.serialization.Serializable

@Serializable
enum class AppRoutingMode {
    AllApps,
    OnlySelectedApps,
    ExcludeSelectedApps,
}

/**
 * Defines which installed applications Android routes into the VPN interface.
 *
 * The policy is applied when TUN is established. Changing it does not mutate
 * an already running VPN session.
 */
@Serializable
data class AppRoutingPolicy(
    val mode: AppRoutingMode = AppRoutingMode.ExcludeSelectedApps,
    val packageNames: Set<String> = emptySet(),
    val allProxy: Boolean = mode == AppRoutingMode.AllApps,
) {
    init {
        require(packageNames.all(PACKAGE_NAME_PATTERN::matches)) {
            "Application routing contains an invalid package name"
        }
        require(allProxy || (mode != AppRoutingMode.AllApps && packageNames.isNotEmpty())) {
            "Selected-app routing requires at least one package"
        }
    }

    companion object {
        val AllApps = AppRoutingPolicy(allProxy = true)

        private val PACKAGE_NAME_PATTERN =
            Regex("[A-Za-z0-9_]+(?:\\.[A-Za-z0-9_]+)+")
    }
}
