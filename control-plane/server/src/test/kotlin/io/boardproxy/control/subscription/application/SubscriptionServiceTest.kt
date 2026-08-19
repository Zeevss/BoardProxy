package io.boardproxy.control.subscription.application

import io.boardproxy.control.shared.contracts.KeylinkQueries
import io.boardproxy.control.shared.contracts.NodeKeylink
import io.boardproxy.control.shared.contracts.UserUsage
import io.boardproxy.control.shared.contracts.UserUsageQueries
import io.boardproxy.control.shared.errors.ResourceForbidden
import io.boardproxy.control.shared.errors.ResourceGone
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.subscription.domain.Subscription
import io.boardproxy.control.subscription.domain.SubscriptionState
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertNull
import kotlin.test.assertTrue

class SubscriptionServiceTest {
    private val now = Instant.parse("2026-08-18T12:00:00Z")
    private val subscriptions = FakeSubscriptions()
    private val keylinks = FakeKeylinks()
    private val usage = FakeUsage()

    private val service = SubscriptionService(
        subscriptions = subscriptions,
        keylinks = keylinks,
        usage = usage,
        audit = { },
        transactions = object : TransactionRunner {
            override fun <T> required(block: () -> T): T = block()
        },
        links = object : SubscriptionLinkBuilder {
            override val enabled = false
            override fun build(issued: IssuedSubscription) = error("delivery disabled")
        },
        clock = Clock.fixed(now, ZoneOffset.UTC),
    )

    /**
     * Главное новое свойство: подписка не хранит список нод. Появился грант —
     * ключ появился в резолве без правки самой подписки.
     */
    @Test
    fun `новый грант немедленно виден в резолве`() {
        val issued = service.create(SubscriptionDraft("Алиса", "u1"), "operator")
        keylinks.links = listOf(NodeKeylink("node-1", "Node 1", "bproxy://a@hash-1#Алиса"))

        assertEquals(listOf("node-1"), service.resolve(issued.token, null).keys.map { it.nodeId })

        keylinks.links = keylinks.links + NodeKeylink("node-2", "Node 2", "bproxy://b@hash-2#Алиса")

        assertEquals(listOf("node-1", "node-2"), service.resolve(issued.token, null).keys.map { it.nodeId })
    }

    @Test
    fun `лимит трафика попадает в снимок`() {
        val issued = service.create(SubscriptionDraft("Алиса", "u1"), "operator")
        keylinks.links = listOf(NodeKeylink("node-1", "Node 1", "bproxy://a@hash-1#Алиса"))
        usage.value = UserUsage(limitBytes = 1_000, usedBytes = 400, perNode = mapOf("node-1" to 400))

        val snapshot = service.resolve(issued.token, null)

        assertEquals(1_000, snapshot.trafficLimit)
        assertEquals(400, snapshot.usedBytes)
        assertEquals(400, snapshot.keys.single().usedBytes)
    }

    @Test
    fun `нода без рабочей ссылки отдаётся выключенной`() {
        val issued = service.create(SubscriptionDraft("Алиса", "u1"), "operator")
        keylinks.links = listOf(NodeKeylink("node-1", "Node 1", null))

        val key = service.resolve(issued.token, null).keys.single()

        assertEquals("disabled", key.state)
        assertNull(key.keylink)
    }

    @Test
    fun `ротация обесценивает прежний токен`() {
        val issued = service.create(SubscriptionDraft("Алиса", "u1"), "operator")
        val rotated = service.rotate(issued.subscription.id, issued.subscription.version, "operator")

        assertTrue(rotated.token != issued.token)
        assertFailsWith<io.boardproxy.control.shared.errors.ResourceNotFound> {
            service.resolve(issued.token, null)
        }
    }

    @Test
    fun `выключенная и отозванная подписка не резолвятся`() {
        val issued = service.create(SubscriptionDraft("Алиса", "u1"), "operator")
        service.replace(
            issued.subscription.id, issued.subscription.version,
            SubscriptionReplacement("Алиса", SubscriptionState.DISABLED), "operator",
        )
        assertFailsWith<ResourceForbidden> { service.resolve(issued.token, null) }

        service.replace(
            issued.subscription.id, issued.subscription.version + 1,
            SubscriptionReplacement("Алиса", SubscriptionState.REVOKED), "operator",
        )
        assertFailsWith<ResourceGone> { service.resolve(issued.token, null) }
    }

    private class FakeKeylinks : KeylinkQueries {
        var links: List<NodeKeylink> = emptyList()
        override fun forUser(userId: String, label: String) = links
    }

    private class FakeUsage : UserUsageQueries {
        var value = UserUsage(0, 0, emptyMap())
        override fun usage(userId: String) = value
    }

    private class FakeSubscriptions : SubscriptionRepository {
        private val stored = mutableMapOf<String, Subscription>()
        private val secrets = mutableMapOf<String, SubscriptionSecrets>()

        override fun create(subscription: Subscription, secrets: SubscriptionSecrets) {
            stored[subscription.id] = subscription
            this.secrets[subscription.id] = secrets
        }

        override fun replace(subscription: Subscription, expectedVersion: Long): Boolean {
            if (stored[subscription.id]?.version != expectedVersion) return false
            stored[subscription.id] = subscription
            return true
        }

        override fun rotateSecrets(
            subscription: Subscription,
            expectedVersion: Long,
            secrets: SubscriptionSecrets,
        ): Boolean {
            if (stored[subscription.id]?.version != expectedVersion) return false
            stored[subscription.id] = subscription
            this.secrets[subscription.id] = secrets
            return true
        }

        override fun findSecrets(id: String) = secrets[id]
        override fun find(id: String) = stored[id]
        override fun findByTokenHash(tokenHash: String) = stored.values.firstOrNull { it.tokenHash == tokenHash }
        override fun findByRecoveryPublicKey(publicKey: String) =
            stored.values.firstOrNull { it.recoveryPublicKey == publicKey }

        override fun list(userId: String?, offset: Int, limit: Int) =
            stored.values.filter { userId == null || it.userId == userId }.drop(offset).take(limit)

        override fun count(userId: String?) =
            stored.values.count { userId == null || it.userId == userId }.toLong()

        override fun delete(id: String, expectedVersion: Long): Boolean {
            if (stored[id]?.version != expectedVersion) return false
            stored.remove(id)
            return true
        }
    }
}
