package ru.zevsus.proxy.boardvpn.infrastructure.subscription

import io.boardproxy.mobile.Mobile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import ru.zevsus.proxy.boardvpn.domain.model.BoardProxyKeylink
import ru.zevsus.proxy.boardvpn.domain.model.BoardProxySubscriptionUrl
import ru.zevsus.proxy.boardvpn.domain.model.SubscriptionKeySummary
import ru.zevsus.proxy.boardvpn.domain.model.VpnSubscription
import ru.zevsus.proxy.boardvpn.domain.repository.ResolvedSubscription
import ru.zevsus.proxy.boardvpn.domain.repository.SubscriptionRepository

class AarSubscriptionRepository : SubscriptionRepository {
    override suspend fun resolve(
        url: BoardProxySubscriptionUrl,
        preferredKeyId: String?,
    ): ResolvedSubscription =
        withContext(Dispatchers.IO) {
            val root = JSONObject(Mobile.resolveSubscription(url.reveal()))
            val keysJson = root.getJSONArray("keys")
            val summaries = buildList {
                for (index in 0 until keysJson.length()) {
                    val key = keysJson.getJSONObject(index)
                    add(
                        SubscriptionKeySummary(
                            id = key.getString("id"),
                            name = key.optString("name"),
                            nodeId = key.optString("nodeId"),
                            state = key.optString("state"),
                            usedBytes = key.optLong("usedBytes"),
                        )
                    )
                }
            }
            val enabledKeys = (0 until keysJson.length())
                .asSequence()
                .map(keysJson::getJSONObject)
                .filter { key ->
                    key.optString("state") == "enabled" && key.optString("keylink").isNotBlank()
                }
                .toList()
            val selected = enabledKeys.firstOrNull { key ->
                preferredKeyId != null && key.optString("id") == preferredKeyId
            } ?: enabledKeys.firstOrNull()
                ?: error("Subscription does not contain an enabled key")
            val selectedKeylink = BoardProxyKeylink.fromRaw(selected.getString("keylink"))
            ResolvedSubscription(
                name = root.optString("name").ifBlank { "BoardProxy subscription" },
                selectedKeylink = selectedKeylink,
                selectedKeyId = selected.getString("id"),
                metadata = VpnSubscription(
                    url = url,
                    id = root.getString("id"),
                    revision = root.optString("revision"),
                    keys = summaries,
                    selectedKeyId = selected.getString("id"),
                ),
            )
        }
}
