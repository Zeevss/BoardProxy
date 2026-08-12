package io.boardproxy.control.delivery.application

import io.boardproxy.control.delivery.domain.AppliedState
import io.boardproxy.control.delivery.domain.ApplyOutcome
import io.boardproxy.control.delivery.domain.NodeHello
import io.boardproxy.control.delivery.domain.NodeStatus
import io.boardproxy.control.provisioning.application.ConfigRevisionRepository
import io.boardproxy.control.provisioning.domain.model.ConfigRevision
import java.time.Instant
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

class NodeSessionTest {
    private val now = Instant.parse("2026-03-01T10:00:00Z")

    @Test
    fun `connect delivers desired once and successful apply converges status`() {
        val revisions = Revisions(revision(2, "new"))
        val statuses = Statuses()
        val session = NodeSession("node-1", revisions, statuses, NodeStatusNotifier { }, AppliedState(1, "old"))

        session.connected(NodeHello("boot-1", "agent", "core", 1, "old"), now)
        assertNotNull(session.pendingDesired(now))
        assertNull(session.pendingDesired(now.plusSeconds(1)))
        session.recordApply(ApplyOutcome(2, 7, "new", "", now.plusSeconds(2)), now.plusSeconds(2))

        val status = requireNotNull(statuses.value)
        assertTrue(status.connected)
        assertEquals(2, status.desiredRevision)
        assertEquals(2, status.appliedRevision)
        assertEquals("new", status.configSha256)
    }

    @Test
    fun `disconnect from older boot cannot overwrite newer session`() {
        val revisions = Revisions(null)
        val statuses = Statuses(NodeStatus("node-1", connected = true, bootId = "boot-2", version = 3))
        val old = NodeSession("node-1", revisions, statuses, NodeStatusNotifier { }, AppliedState())
        old.connected(NodeHello("boot-1", "agent", "core", 0, ""), now)
        statuses.value = requireNotNull(statuses.value).copy(connected = true, bootId = "boot-2", version = 5)

        old.disconnected(now.plusSeconds(1))

        assertTrue(requireNotNull(statuses.value).connected)
        assertEquals("boot-2", statuses.value?.bootId)
        assertFalse(requireNotNull(statuses.value).coreReady)
    }

    private class Revisions(var value: ConfigRevision?) : ConfigRevisionRepository {
        override fun append(
            nodeId: String,
            catalogVersion: Long,
            configToml: ByteArray,
            cause: String,
            createdAt: Instant,
        ): ConfigRevision = error("not used")

        override fun latest(nodeId: String): ConfigRevision? = value
    }

    private class Statuses(var value: NodeStatus? = null) : NodeStatusRepository {
        override fun find(nodeId: String): NodeStatus? = value

        override fun save(status: NodeStatus, expectedVersion: Long): Boolean {
            if ((value?.version ?: 0) != expectedVersion) return false
            value = status
            return true
        }
    }

    private fun revision(value: Long, hash: String) = ConfigRevision(
        nodeId = "node-1", revision = value, previousRevision = value - 1,
        catalogVersion = value, configToml = "config".toByteArray(), configSha256 = hash,
        cause = "test", createdAt = now,
    )
}
