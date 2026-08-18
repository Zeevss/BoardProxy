package io.boardproxy.control.subscriber.application

import io.boardproxy.control.provisioning.application.CatalogQueries
import io.boardproxy.control.provisioning.application.CatalogResourceCommands
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.subscriber.domain.UserPlacement
import io.boardproxy.control.telemetry.application.TrafficQuotaNotifier
import io.boardproxy.control.telemetry.application.TrafficQuotaRepository
import io.boardproxy.control.telemetry.application.TrafficQuotaService
import io.boardproxy.control.telemetry.domain.QuotaAction
import io.boardproxy.control.telemetry.domain.QuotaPeriod
import io.boardproxy.control.telemetry.domain.TrafficQuota
import io.boardproxy.control.testing.TestCatalogs
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

class FleetUserServiceTest {
    private val now = Instant.parse("2026-08-12T12:00:00Z")

    @Test
    fun `sums per-node quotas into one fleet traffic limit`() {
        val quotas = Quotas(usedBytes = 30)
        quotas.values += TrafficQuota("node-a", "alice", QuotaPeriod.MONTHLY, 100, QuotaAction.ALERT, true, 1, now)
        quotas.values += TrafficQuota("node-b", "alice", QuotaPeriod.MONTHLY, 100, QuotaAction.ALERT, true, 1, now)

        val user = service(quotas, listOf(record("alice", "node-a", "node-b"))).list(null).single()

        // Квота применяется на каждой ноде отдельно, поэтому суммарный лимит флота — сумма.
        assertEquals(200, user.limits.traffic?.limitBytes)
        assertEquals(60, user.limits.traffic?.usedBytes)
        assertEquals(QuotaPeriod.MONTHLY, user.limits.traffic?.period)
    }

    @Test
    fun `user without a quota has no traffic limit`() {
        val user = service(Quotas(usedBytes = 0), listOf(record("bob", "node-a"))).list(null).single()

        assertNull(user.limits.traffic)
        assertEquals(2, user.limits.maxDevices)
        assertEquals(4, user.limits.maxPages)
    }

    @Test
    fun `a user disabled on one node is still reachable through another`() {
        val record = FleetUserRecord(
            id = "carol", name = "Carol", state = ResourceState.ENABLED,
            placements = listOf(
                UserPlacement("node-a", "A", ResourceState.DISABLED, emptyList(), 1),
                UserPlacement("node-b", "B", ResourceState.ENABLED, emptyList(), 1),
            ),
            maxDevices = 2, maxPages = 4, subscription = null, updatedAt = now,
        )

        val user = service(Quotas(usedBytes = 0), listOf(record)).list(null).single()

        assertTrue(user.enabledSomewhere)
    }

    private fun record(id: String, vararg nodes: String) = FleetUserRecord(
        id = id, name = id.replaceFirstChar(Char::uppercase), state = ResourceState.ENABLED,
        placements = nodes.map { UserPlacement(it, it.uppercase(), ResourceState.ENABLED, emptyList(), 1) },
        maxDevices = 2, maxPages = 4, subscription = null, updatedAt = now,
    )

    private fun service(quotas: Quotas, records: List<FleetUserRecord>) = FleetUserService(
        { records },
        TrafficQuotaService(
            quotas,
            CatalogQueries { TestCatalogs.catalog() },
            NoResources(),
            TrafficQuotaNotifier {},
            Clock.fixed(now, ZoneOffset.UTC),
        ),
    )

    private class NoResources : CatalogResourceCommands {
        override fun updateNode(nodeId: String, expectedVersion: Long, input: io.boardproxy.control.provisioning.application.NodeInput, actor: String) = error("not used")
        override fun putBoard(nodeId: String, boardId: String, expectedVersion: Long, input: io.boardproxy.control.provisioning.application.BoardInput, actor: String) = error("not used")
        override fun removeBoard(nodeId: String, boardId: String, expectedVersion: Long, actor: String) = error("not used")
        override fun putUser(nodeId: String, userId: String, expectedVersion: Long, input: io.boardproxy.control.provisioning.application.UserInput, actor: String) = error("not used")
        override fun removeUser(nodeId: String, userId: String, expectedVersion: Long, actor: String) = error("not used")
        override fun replaceAssignment(nodeId: String, expectedVersion: Long, boardIds: List<String>, users: List<io.boardproxy.control.provisioning.domain.model.AssignedUser>, actor: String) = error("not used")
    }

    private class Quotas(private val usedBytes: Long) : TrafficQuotaRepository {
        val values = mutableListOf<TrafficQuota>()
        override fun find(nodeId: String, userTag: String) = values.firstOrNull { it.nodeId == nodeId && it.userTag == userTag }
        override fun list(nodeId: String) = values.filter { it.nodeId == nodeId }
        override fun save(quota: TrafficQuota, expectedVersion: Long?) = true
        override fun delete(nodeId: String, userTag: String, expectedVersion: Long) = true
        override fun enabled() = values.filter { it.enabled }
        override fun usedBytes(nodeId: String, userTag: String, from: Instant, to: Instant) = usedBytes
        override fun recordExceeded(nodeId: String, userTag: String, periodStart: Instant, at: Instant) = true
        override fun recordEnforced(nodeId: String, userTag: String, periodStart: Instant, at: Instant) = Unit
        override fun state(nodeId: String, userTag: String, periodStart: Instant) = null to null
        override fun startNewCounter(nodeId: String, userTag: String, at: Instant) = Unit
    }
}
