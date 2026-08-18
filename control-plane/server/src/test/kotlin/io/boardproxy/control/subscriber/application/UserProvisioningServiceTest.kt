package io.boardproxy.control.subscriber.application

import io.boardproxy.control.provisioning.application.CatalogCommands
import io.boardproxy.control.provisioning.application.CatalogMutationResult
import io.boardproxy.control.provisioning.application.CatalogQueries
import io.boardproxy.control.provisioning.domain.model.Catalog
import io.boardproxy.control.provisioning.domain.model.ConfigRevision
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.telemetry.domain.QuotaAction
import io.boardproxy.control.telemetry.domain.QuotaPeriod
import io.boardproxy.control.telemetry.domain.TrafficQuota
import io.boardproxy.control.subscription.application.IssuedSubscription
import io.boardproxy.control.subscription.application.SubscriptionCommands
import io.boardproxy.control.subscription.application.SubscriptionDraft
import io.boardproxy.control.subscription.application.SubscriptionLinkBuilder
import io.boardproxy.control.subscription.application.SubscriptionReplacement
import io.boardproxy.control.subscription.domain.Subscription
import io.boardproxy.control.subscription.domain.SubscriptionKey
import io.boardproxy.control.subscription.domain.SubscriptionState
import io.boardproxy.control.testing.TestCatalogs
import java.security.SecureRandom
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import java.util.Base64
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertTrue

class UserProvisioningServiceTest {
    private val now = Instant.parse("2026-08-15T12:00:00Z")

    @Test
    fun `enabled subscription provisions one user on two nodes and returns one URL`() {
        val fixture = Fixture(subscriptionEnabled = true)

        val result = fixture.service.create(request(), "operator")

        assertEquals("subscription", result.delivery.type)
        assertEquals("https://subscribe.example/s/token#bp1=capsule", result.delivery.subscriptionUrl)
        assertTrue(result.delivery.keys.isEmpty())
        assertEquals(listOf("node-1", "node-2"), fixture.subscriptionDraft?.keys?.map { it.nodeId })
        fixture.catalogs.values.forEach { catalog ->
            assertNotNull(catalog.users.firstOrNull { it.id == "alice" })
            assertTrue(catalog.assignment.users.any { it.userId == "alice" })
        }
        val privateKeys = fixture.catalogs.values.map { it.users.first { user -> user.id == "alice" }.privateKey }.toSet()
        assertEquals(1, privateKeys.size, "the same client identity should be provisioned on every target node")
        assertEquals(1, fixture.transactions)
    }

    @Test
    fun `disabled subscription mode returns legacy keylinks`() {
        val fixture = Fixture(subscriptionEnabled = false)

        val result = fixture.service.create(request(), "operator")

        assertEquals("keylinks", result.delivery.type)
        assertEquals(2, result.delivery.keys.size)
        assertTrue(result.delivery.keys.all { it.keylink.startsWith("bproxy://") })
        assertEquals(null, fixture.subscriptionDraft)
    }

    @Test
    fun `traffic limit is applied to every node together with the user`() {
        val fixture = Fixture(subscriptionEnabled = true)

        fixture.service.create(
            request().copy(traffic = TrafficLimitRequest(50, QuotaPeriod.WEEKLY, QuotaAction.RESET)),
            "operator",
        )

        // Квота должна лечь на обе ноды в той же транзакции, что и сам пользователь.
        assertEquals(listOf("node-1", "node-2"), fixture.appliedQuotas.map { it.nodeId })
        assertTrue(fixture.appliedQuotas.all { it.limitBytes == 50L && it.period == QuotaPeriod.WEEKLY })
        assertEquals(1, fixture.transactions)
    }

    @Test
    fun `user without a traffic limit gets no quota at all`() {
        val fixture = Fixture(subscriptionEnabled = true)

        fixture.service.create(request(), "operator")

        assertTrue(fixture.appliedQuotas.isEmpty())
    }

    private fun request() = UserProvisioningRequest(
        id = "alice", name = "Alice",
        targets = listOf(
            UserTarget("node-1", listOf("board-1"), "Primary"),
            UserTarget("node-2", listOf("board-1"), "Backup"),
        ),
        maxSessions = 2, maxLanes = 3,
    )

    private inner class Fixture(subscriptionEnabled: Boolean) : CatalogQueries, CatalogCommands, SubscriptionCommands {
        val catalogs = linkedMapOf(
            "node-1" to TestCatalogs.catalog(now = now).withoutAlice(),
            "node-2" to TestCatalogs.catalog(now = now).copy(
                node = TestCatalogs.catalog(now = now).node.copy(id = "node-2", name = "Secondary"),
                assignment = TestCatalogs.catalog(now = now).assignment.copy(nodeId = "node-2", users = emptyList()),
                users = emptyList(),
            ),
        )
        var subscriptionDraft: SubscriptionDraft? = null
        val appliedQuotas = mutableListOf<TrafficQuota>()
        var transactions = 0
        val service = UserProvisioningService(
            catalogs = this, catalogCommands = this, subscriptions = this,
            quotas = { nodeId, userTag, period, limitBytes, action, enabled, _ ->
                TrafficQuota(nodeId, userTag, period, limitBytes, action, enabled, 1, now)
                    .also { appliedQuotas += it }
            },
            links = object : SubscriptionLinkBuilder {
                override val enabled = subscriptionEnabled
                override fun build(issued: IssuedSubscription) = "https://subscribe.example/s/token#bp1=capsule"
            },
            transactions = object : TransactionRunner {
                override fun <T> required(block: () -> T): T {
                    transactions++
                    return block()
                }
            },
            clock = Clock.fixed(now, ZoneOffset.UTC),
            random = object : SecureRandom() {
                override fun nextBytes(bytes: ByteArray) = bytes.indices.forEach { bytes[it] = (it + 7).toByte() }
            },
        )

        override fun get(nodeId: String): Catalog = requireNotNull(catalogs[nodeId])

        override fun create(catalog: Catalog, actor: String): CatalogMutationResult = error("not used")

        override fun replace(
            catalog: Catalog,
            expectedVersion: Long,
            actor: String,
            cause: String,
        ): CatalogMutationResult {
            assertEquals(catalogs.getValue(catalog.node.id).version, expectedVersion)
            catalogs[catalog.node.id] = catalog
            return CatalogMutationResult(
                catalog,
                ConfigRevision(catalog.node.id, 1, 0, catalog.version, byteArrayOf(), "hash", cause, now),
                true,
            )
        }

        override fun create(draft: SubscriptionDraft, actor: String): IssuedSubscription {
            subscriptionDraft = draft
            val public = Base64.getUrlEncoder().withoutPadding().encodeToString(ByteArray(32) { 3 })
            return IssuedSubscription(
                Subscription(
                    id = "subscription-1", name = draft.name, tokenHash = "a".repeat(64),
                    recoveryPublicKey = public, state = SubscriptionState.ENABLED,
                    keys = draft.keys.mapIndexed { index, key ->
                        SubscriptionKey(key.id, key.name, key.nodeId, key.userId, index)
                    },
                    version = 1, createdAt = now, updatedAt = now,
                ),
                token = "token",
                recoveryClientPrivateKey = public,
            )
        }

        override fun replace(
            id: String,
            expectedVersion: Long,
            replacement: SubscriptionReplacement,
            actor: String,
        ): Subscription = error("not used")

        override fun rotate(id: String, expectedVersion: Long, actor: String): IssuedSubscription =
            error("not used")
    }

    private fun Catalog.withoutAlice() = copy(
        users = emptyList(),
        assignment = assignment.copy(users = emptyList()),
    )
}
