package io.boardproxy.control.telemetry.application

import io.boardproxy.control.provisioning.application.CatalogQueries
import io.boardproxy.control.provisioning.application.CatalogResourceCommands
import io.boardproxy.control.provisioning.application.UserInput
import io.boardproxy.control.provisioning.application.CatalogMutationResult
import io.boardproxy.control.provisioning.domain.model.ConfigRevision
import io.boardproxy.control.telemetry.domain.QuotaAction
import io.boardproxy.control.telemetry.domain.QuotaPeriod
import io.boardproxy.control.telemetry.domain.TrafficQuota
import io.boardproxy.control.testing.TestCatalogs
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class TrafficQuotaServiceTest {
    private val now = Instant.parse("2026-08-12T12:00:00Z")

    @Test
    fun `alert quota notifies once and never mutates desired state`() {
        val repository = Quotas(usedBytes = 150)
        val resources = Resources()
        val notifications = mutableListOf<Long>()
        val service = service(repository, resources) { notifications += it.usedBytes }
        repository.values += TrafficQuota("node-1", "user-1", QuotaPeriod.DAILY, 100, QuotaAction.ALERT, true, 1, now)

        service.evaluate()
        service.evaluate()

        assertEquals(listOf(150L), notifications)
        assertEquals(0, resources.calls)
    }

    @Test
    fun `explicit disable quota disables enabled user and records enforcement`() {
        val repository = Quotas(usedBytes = 100)
        val resources = Resources()
        val service = service(repository, resources) {}
        repository.values += TrafficQuota("node-1", "user-1", QuotaPeriod.MONTHLY, 100, QuotaAction.DISABLE, true, 1, now)

        service.evaluate()

        assertEquals(1, resources.calls)
        assertTrue(repository.enforced)
        assertTrue(repository.exceeded)
    }

    private fun service(repository: Quotas, resources: Resources, notify: (io.boardproxy.control.telemetry.domain.TrafficQuotaUsage) -> Unit) =
        TrafficQuotaService(repository, CatalogQueries { TestCatalogs.catalog() }, resources, TrafficQuotaNotifier(notify), Clock.fixed(now, ZoneOffset.UTC))

    private inner class Resources : CatalogResourceCommands {
        var calls = 0
        override fun updateNode(nodeId: String, expectedVersion: Long, input: io.boardproxy.control.provisioning.application.NodeInput, actor: String) = error("not used")
        override fun putBoard(nodeId: String, boardId: String, expectedVersion: Long, input: io.boardproxy.control.provisioning.application.BoardInput, actor: String) = error("not used")
        override fun removeBoard(nodeId: String, boardId: String, expectedVersion: Long, actor: String) = error("not used")
        override fun putUser(nodeId: String, userId: String, expectedVersion: Long, input: UserInput, actor: String): CatalogMutationResult {
            calls++
            val catalog = TestCatalogs.catalog()
            return CatalogMutationResult(
                catalog,
                ConfigRevision(nodeId, 2, 1, catalog.version, byteArrayOf(), "hash", "quota", now),
                true,
            )
        }
        override fun removeUser(nodeId: String, userId: String, expectedVersion: Long, actor: String) = error("not used")
        override fun replaceAssignment(nodeId: String, expectedVersion: Long, boardIds: List<String>, users: List<io.boardproxy.control.provisioning.domain.model.AssignedUser>, actor: String) = error("not used")
    }

    private inner class Quotas(private val usedBytes: Long) : TrafficQuotaRepository {
        val values = mutableListOf<TrafficQuota>()
        var exceeded = false
        var enforced = false
        override fun find(nodeId: String, userTag: String) = values.firstOrNull()
        override fun list(nodeId: String) = values.toList()
        override fun save(quota: TrafficQuota, expectedVersion: Long?) = true
        override fun delete(nodeId: String, userTag: String, expectedVersion: Long) = true
        override fun enabled() = values.filter { it.enabled }
        override fun usedBytes(nodeId: String, userTag: String, from: Instant, to: Instant) = usedBytes
        override fun recordExceeded(nodeId: String, userTag: String, periodStart: Instant, at: Instant): Boolean = (!exceeded).also { exceeded = true }
        override fun recordEnforced(nodeId: String, userTag: String, periodStart: Instant, at: Instant) { enforced = true }
        override fun state(nodeId: String, userTag: String, periodStart: Instant) = (if (exceeded) now else null) to (if (enforced) now else null)
    }
}
