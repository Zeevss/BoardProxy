package ru.zevsus.proxy.boardvpn.domain.subscription

import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import ru.zevsus.proxy.boardvpn.domain.model.BoardProxyKeylink
import ru.zevsus.proxy.boardvpn.domain.model.BoardProxySubscriptionUrl
import ru.zevsus.proxy.boardvpn.domain.model.SubscriptionKeySummary
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.model.VpnSubscription
import ru.zevsus.proxy.boardvpn.domain.repository.ResolvedSubscription
import ru.zevsus.proxy.boardvpn.domain.repository.SubscriptionRepository
import ru.zevsus.proxy.boardvpn.infrastructure.fake.InMemoryVpnProfileRepository

class SubscriptionSyncManagerTest {
    private val oldKey = BoardProxyKeylink.fromRaw("bproxy://${"A".repeat(86)}")
    private val newKey = BoardProxyKeylink.fromRaw("bproxy://${"B".repeat(86)}")
    private val url = BoardProxySubscriptionUrl.fromRaw(
        "https://subscribe.example.com/s/family#bp1=demo"
    )
    private val profile = VpnProfile(
        id = VpnProfileId("family"),
        name = "My name",
        keylink = oldKey,
        subscription = VpnSubscription(url, "family", "r1", emptyList()),
    )

    @Test
    fun `refresh atomically replaces the selected key and metadata`() = runTest {
        val profiles = InMemoryVpnProfileRepository(listOf(profile))
        val manager = SubscriptionSyncManager(
            scope = backgroundScope,
            profiles = profiles,
            subscriptions = successfulRepository(),
            nowMillis = { 42L },
        )

        val report = manager.refreshAll()
        val updated = profiles.getProfile(profile.id)!!

        assertEquals(setOf(profile.id), report.updated)
        assertEquals(newKey, updated.keylink)
        assertEquals("r2", updated.subscription?.revision)
        assertEquals(42L, updated.subscription?.updatedAtEpochMillis)
        assertEquals("My name", updated.name)
        assertTrue(manager.state.value.failed.isEmpty())
    }

    @Test
    fun `failed refresh preserves the last working key`() = runTest {
        val profiles = InMemoryVpnProfileRepository(listOf(profile))
        val manager = SubscriptionSyncManager(
            scope = backgroundScope,
            profiles = profiles,
            subscriptions = object : SubscriptionRepository {
                override suspend fun resolve(
                    url: BoardProxySubscriptionUrl,
                    preferredKeyId: String?,
                ): ResolvedSubscription =
                    error("offline")
            },
        )

        val report = manager.refreshAll()

        assertEquals(setOf(profile.id), report.failed)
        assertEquals(oldKey, profiles.getProfile(profile.id)?.keylink)
        assertEquals(setOf(profile.id), manager.state.value.failed)
    }

    @Test
    fun `fresh subscription is reused before vpn start without a second request`() = runTest {
        var requests = 0
        val freshProfile = profile.copy(
            subscription = profile.subscription?.copy(updatedAtEpochMillis = 90L),
        )
        val profiles = InMemoryVpnProfileRepository(listOf(freshProfile))
        val manager = SubscriptionSyncManager(
            scope = backgroundScope,
            profiles = profiles,
            subscriptions = object : SubscriptionRepository {
                override suspend fun resolve(
                    url: BoardProxySubscriptionUrl,
                    preferredKeyId: String?,
                ): ResolvedSubscription {
                    requests += 1
                    return successfulRepository().resolve(url, preferredKeyId)
                }
            },
            nowMillis = { 100L },
        )

        val result = manager.refreshIfStale(profile.id, maxAgeMillis = 20L)

        assertTrue(result.isSuccess)
        assertEquals(0, requests)
        assertEquals(oldKey, result.getOrNull()?.keylink)
    }

    private fun successfulRepository() = object : SubscriptionRepository {
        override suspend fun resolve(
            url: BoardProxySubscriptionUrl,
            preferredKeyId: String?,
        ) = ResolvedSubscription(
            name = "Server name",
            selectedKeylink = newKey,
            selectedKeyId = "de",
            metadata = VpnSubscription(
                url = url,
                id = "family",
                revision = "r2",
                keys = listOf(
                    SubscriptionKeySummary("de", "Germany", "node-1", "enabled", 100),
                ),
                selectedKeyId = "de",
            ),
        )
    }
}
