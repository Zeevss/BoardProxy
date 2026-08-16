package ru.zevsus.proxy.boardvpn.domain.repository

import ru.zevsus.proxy.boardvpn.domain.model.BoardProxyKeylink
import ru.zevsus.proxy.boardvpn.domain.model.BoardProxySubscriptionUrl
import ru.zevsus.proxy.boardvpn.domain.model.VpnSubscription

data class ResolvedSubscription(
    val name: String,
    val selectedKeylink: BoardProxyKeylink,
    val selectedKeyId: String,
    val metadata: VpnSubscription,
)

interface SubscriptionRepository {
    suspend fun resolve(
        url: BoardProxySubscriptionUrl,
        preferredKeyId: String? = null,
    ): ResolvedSubscription
}
