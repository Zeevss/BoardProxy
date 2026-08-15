package io.boardproxy.control.subscription.application

import io.boardproxy.control.audit.application.AuditRepository
import io.boardproxy.control.audit.domain.AuditEvent
import io.boardproxy.control.provisioning.application.CatalogQueries
import io.boardproxy.control.provisioning.domain.model.AssignedUser
import io.boardproxy.control.provisioning.domain.model.User
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.subscription.domain.Subscription
import io.boardproxy.control.telemetry.application.TrafficKind
import io.boardproxy.control.telemetry.application.TrafficPoint
import io.boardproxy.control.telemetry.application.TrafficQueries
import io.boardproxy.control.telemetry.application.TrafficTotal
import io.boardproxy.control.testing.TestCatalogs
import java.security.SecureRandom
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertTrue

class SubscriptionServiceTest {
    private val now = Instant.parse("2026-08-15T12:00:00Z")

    @Test
    fun `one subscription resolves multiple ordered keylinks and traffic totals`() {
        val base = TestCatalogs.catalog(now = now)
        val secondUser = User(
            id = "user-2", name = "Bob", privateKey = TestCatalogs.key(3),
            state = base.users.single().state, maxSessions = 2, maxLanes = 3,
            version = 1, updatedAt = now,
        )
        val catalog = base.copy(
            users = base.users + secondUser,
            assignment = base.assignment.copy(
                users = base.assignment.users + AssignedUser("user-2", listOf("board-1")),
            ),
        )
        val repository = MemoryRepository()
        val audit = mutableListOf<AuditEvent>()
        var transactions = 0
        var id = 0
        val service = SubscriptionService(
            subscriptions = repository,
            catalogs = CatalogQueries { catalog },
            traffic = object : TrafficQueries {
                override fun interfaceTotals(nodeId: String, from: Instant, to: Instant) = emptyList<TrafficTotal>()
                override fun userTotals(nodeId: String, from: Instant, to: Instant) = listOf(
                    TrafficTotal("user-1", 10, 20),
                    TrafficTotal("user-2", 30, 40),
                )
                override fun series(
                    nodeId: String,
                    kind: TrafficKind,
                    from: Instant,
                    to: Instant,
                    bucketSeconds: Long,
                ) = emptyList<TrafficPoint>()
            },
            audit = AuditRepository(audit::add),
            transactions = object : TransactionRunner {
                override fun <T> required(block: () -> T): T {
                    transactions++
                    return block()
                }
            },
            clock = Clock.fixed(now, ZoneOffset.UTC),
            random = object : SecureRandom() {
                override fun nextBytes(bytes: ByteArray) = bytes.indices.forEach { bytes[it] = (it + 1).toByte() }
            },
            nextId = { "subscription-${++id}" },
        )

        val issued = service.create(
            SubscriptionDraft(
                "Family",
                listOf(
                    SubscriptionKeyDraft("alice", "Alice phone", "node-1", "user-1"),
                    SubscriptionKeyDraft("bob", "Bob laptop", "node-1", "user-2"),
                ),
            ),
            "operator",
        )
        val snapshot = service.resolve(issued.token, null)

        assertEquals(listOf("alice", "bob"), snapshot.keys.map { it.id })
        assertEquals(listOf(30L, 70L), snapshot.keys.map { it.usedBytes })
        assertEquals(100L, snapshot.usedBytes)
        assertTrue(snapshot.keys.all { it.state == "enabled" && it.keylink?.startsWith("bproxy://") == true })
        assertEquals(1, transactions)
        assertEquals("subscription.created", audit.single().action)
        assertNotNull(repository.findByRecoveryPublicKey(issued.subscription.recoveryPublicKey))
    }

    private class MemoryRepository : SubscriptionRepository {
        private val values = linkedMapOf<String, Subscription>()

        override fun create(subscription: Subscription) {
            values[subscription.id] = subscription
        }

        override fun replace(subscription: Subscription, expectedVersion: Long): Boolean {
            if (values[subscription.id]?.version != expectedVersion) return false
            values[subscription.id] = subscription
            return true
        }

        override fun find(id: String): Subscription? = values[id]
        override fun findByTokenHash(tokenHash: String): Subscription? = values.values.firstOrNull { it.tokenHash == tokenHash }
        override fun findByRecoveryPublicKey(publicKey: String): Subscription? =
            values.values.firstOrNull { it.recoveryPublicKey == publicKey }
        override fun list(): List<Subscription> = values.values.toList()
    }
}
