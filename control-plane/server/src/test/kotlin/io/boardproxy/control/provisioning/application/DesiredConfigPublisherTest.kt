package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.NodeState
import io.boardproxy.control.provisioning.domain.model.UserPlacement
import io.boardproxy.control.shared.audit.AuditEvent
import io.boardproxy.control.shared.audit.AuditRepository
import io.boardproxy.control.shared.contracts.QuotaExceededQueries
import io.boardproxy.control.shared.events.OutboxEvent
import io.boardproxy.control.shared.events.OutboxRepository
import io.boardproxy.control.testing.TEST_TIME
import io.boardproxy.control.testing.testBoard
import io.boardproxy.control.testing.testNode
import io.boardproxy.control.testing.testUser
import java.time.Clock
import java.time.ZoneOffset
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * Публикация — единственная точка записи desired state, и её главное свойство
 * в том, что ревизия появляется только когда изменились байты TOML. Прежняя
 * схема писала ревизию на каждую замену каталога и определяла факт изменения
 * косвенно, по срабатыванию ON CONFLICT.
 */
class DesiredConfigPublisherTest {
    private val states = FakeStates()
    private val configs = FakeConfigs()
    private val snapshots = FakeSnapshots()
    private val outbox = FakeOutbox()
    private val audit = FakeAudit()
    private var exceeded = emptySet<String>()

    private val publisher = DesiredConfigPublisher(
        states = states,
        quotas = QuotaExceededQueries { exceeded },
        compiler = { input ->
            // Достаточно любого детерминированного представления входа:
            // проверяется реакция на изменение байт, а не формат TOML.
            buildString {
                append(input.node.name).append(':').append(input.node.state)
                input.boards.sortedBy { it.id }.forEach { append('|').append(it.id).append(it.state) }
                input.users.sortedBy { it.user.id }.forEach { append('#').append(it.user.id).append(it.enabled) }
            }.toByteArray()
        },
        configs = configs,
        snapshots = snapshots,
        audit = audit,
        outbox = outbox,
        clock = Clock.fixed(TEST_TIME, ZoneOffset.UTC),
        nextId = { "event-${outbox.events.size + audit.events.size + 1}" },
    )

    @Test
    fun `первая публикация создаёт ревизию 1 и будит ноду`() {
        states.put(state("node-1"))

        val result = publisher.publish(setOf("node-1"), "node.created", "operator").single()

        assertTrue(result.changed)
        assertEquals(1, result.revision)
        assertEquals(listOf("desired-state.changed"), outbox.events.map(OutboxEvent::type))
        assertEquals(1, snapshots.saved.size)
    }

    /** Главное: правка, не меняющая TOML, не должна порождать ревизию и будить ядро. */
    @Test
    fun `повторная публикация без изменений не создаёт ревизию`() {
        states.put(state("node-1"))
        publisher.publish(setOf("node-1"), "node.created", "operator")
        outbox.events.clear()
        snapshots.saved.clear()

        val result = publisher.publish(setOf("node-1"), "node.updated", "operator").single()

        assertFalse(result.changed)
        assertEquals(1, result.revision, "ревизия осталась прежней")
        assertTrue(outbox.events.isEmpty(), "будить ноду незачем")
        assertTrue(snapshots.saved.isEmpty(), "снимок пишется только вместе с ревизией")
    }

    @Test
    fun `изменение содержимого поднимает ревизию`() {
        states.put(state("node-1"))
        publisher.publish(setOf("node-1"), "node.created", "operator")

        states.put(state("node-1", boards = listOf(testBoard("node-1", "board-2", "hash-2"))))
        val result = publisher.publish(setOf("node-1"), "board.added", "operator").single()

        assertTrue(result.changed)
        assertEquals(2, result.revision)
    }

    /**
     * Пользователь на трёх нодах — три конфигурации, а не три полные замены
     * агрегата с шестью снимками, как было раньше.
     */
    @Test
    fun `правка пользователя на трёх нодах создаёт ровно три ревизии`() {
        listOf("node-1", "node-2", "node-3").forEach { states.put(state(it)) }

        val results = publisher.publish(setOf("node-1", "node-2", "node-3"), "user.updated", "operator")

        assertEquals(3, results.size)
        assertTrue(results.all { it.changed })
        assertTrue(results.all { it.revision == 1L })
        assertEquals(3, outbox.events.size)
        assertEquals(3, snapshots.saved.size)
    }

    @Test
    fun `порядок публикации не зависит от порядка входного множества`() {
        listOf("node-1", "node-2").forEach { states.put(state(it)) }

        val direct = publisher.publish(setOf("node-1", "node-2"), "user.updated", "operator").map { it.nodeId }
        val reversed = publisher.publish(setOf("node-2", "node-1"), "user.updated", "operator").map { it.nodeId }

        assertEquals(listOf("node-1", "node-2"), direct)
        assertEquals(direct, reversed)
    }

    /** Квота — вход компиляции: её изменение меняет байты и потому рождает ревизию. */
    @Test
    fun `исчерпание и снятие квоты меняют конфигурацию в обе стороны`() {
        states.put(state("node-1", placements = listOf(UserPlacement(testUser("user-1"), setOf("board-1")))))
        publisher.publish(setOf("node-1"), "node.created", "operator")

        exceeded = setOf("user-1")
        val blocked = publisher.publish(setOf("node-1"), "quota.changed", "system:traffic-quota").single()
        assertTrue(blocked.changed)
        assertEquals(2, blocked.revision)

        exceeded = emptySet()
        val restored = publisher.publish(setOf("node-1"), "quota.changed", "system:traffic-quota").single()
        assertTrue(restored.changed, "снятие превышения обязано вернуть конфигурацию")
        assertEquals(3, restored.revision)
    }

    @Test
    fun `пустое множество нод не делает ничего`() {
        assertTrue(publisher.publish(emptySet(), "noop", "operator").isEmpty())
        assertTrue(outbox.events.isEmpty())
    }

    private fun state(
        nodeId: String,
        boards: List<io.boardproxy.control.provisioning.domain.model.Board> = listOf(testBoard(nodeId)),
        placements: List<UserPlacement> = emptyList(),
    ) = NodeState(testNode(nodeId), boards, placements)

    private class FakeStates : NodeStateLoader {
        private val states = mutableMapOf<String, NodeState>()
        fun put(state: NodeState) { states[state.node.id] = state }
        override fun load(nodeId: String) = states.getValue(nodeId)
    }

    private class FakeConfigs : DesiredConfigRepository {
        private val stored = mutableMapOf<String, DesiredConfig>()
        override fun lock(nodeId: String) = Unit
        override fun find(nodeId: String) = stored[nodeId]
        override fun save(config: DesiredConfig) { stored[config.nodeId] = config }
    }

    private class FakeSnapshots : NodeSnapshotRepository {
        val saved = mutableListOf<NodeState>()
        override fun save(state: NodeState, cause: String, actor: String, at: java.time.Instant): Long {
            saved += state
            return saved.size.toLong()
        }
        override fun find(nodeId: String, seq: Long): NodeState? = saved.getOrNull((seq - 1).toInt())
        override fun list(nodeId: String, offset: Int, limit: Int) = emptyList<NodeSnapshotMetadata>()
        override fun count(nodeId: String) = saved.size.toLong()
    }

    private class FakeOutbox : OutboxRepository {
        val events = mutableListOf<OutboxEvent>()
        override fun append(event: OutboxEvent) { events += event }
    }

    private class FakeAudit : AuditRepository {
        val events = mutableListOf<AuditEvent>()
        override fun append(event: AuditEvent) { events += event }
    }
}
