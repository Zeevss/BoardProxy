package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.Board
import io.boardproxy.control.provisioning.domain.model.NodeState
import io.boardproxy.control.provisioning.domain.model.UserPlacement
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.errors.ResourceNotFound
import io.boardproxy.control.shared.persistence.TransactionRunner
import java.time.Clock

data class NodeConfigChange(val path: String, val before: String?, val after: String?)

data class NodeConfigDiff(
    val nodeId: String,
    val fromSeq: Long,
    val toSeq: Long,
    val changes: List<NodeConfigChange>,
)

interface NodeHistoryQueries {
    fun history(nodeId: String, offset: Int, limit: Int): Page<NodeSnapshotMetadata>
    fun diff(nodeId: String, fromSeq: Long, toSeq: Long): NodeConfigDiff
}

fun interface NodeHistoryCommands {
    fun rollback(nodeId: String, seq: Long, actor: String): PublishResult
}

/**
 * История ноды поверх снимков владеемого состояния.
 *
 * Diff считается по снимкам, а не по скомпилированному TOML: сравнивать то, что
 * задал оператор, осмысленнее, чем результат компиляции. Приватные ключи в diff
 * не попадают вовсе — панель их не видит.
 */
class NodeHistoryService(
    private val snapshots: NodeSnapshotRepository,
    private val nodes: NodeRepository,
    private val boards: BoardRepository,
    private val users: UserRepository,
    private val grants: GrantRepository,
    private val publisher: DesiredConfigPublisher,
    private val transactions: TransactionRunner,
    private val clock: Clock,
) : NodeHistoryQueries, NodeHistoryCommands {

    override fun history(nodeId: String, offset: Int, limit: Int): Page<NodeSnapshotMetadata> {
        nodes.find(nodeId) ?: throw ResourceNotFound("node $nodeId not found")
        return Page(snapshots.list(nodeId, offset, limit), offset, limit, snapshots.count(nodeId))
    }

    override fun diff(nodeId: String, fromSeq: Long, toSeq: Long): NodeConfigDiff {
        val from = flatten(snapshot(nodeId, fromSeq))
        val to = flatten(snapshot(nodeId, toSeq))
        val changes = (from.keys + to.keys).sorted().mapNotNull { path ->
            val before = from[path]
            val after = to[path]
            if (before == after) null else NodeConfigChange(path, before, after)
        }
        return NodeConfigDiff(nodeId, fromSeq, toSeq, changes)
    }

    /**
     * Откат восстанавливает то, что принадлежит ноде: её настройки, борды и
     * размещения. Свойства самих пользователей — имя, ключ, лимиты — общие для
     * всего флота, и откат одной ноды не имеет права их переписывать.
     *
     * Новая конфигурация не «возвращается», а пересобирается: ревизия растёт
     * вперёд, как при любой другой правке.
     */
    override fun rollback(nodeId: String, seq: Long, actor: String): PublishResult = transactions.required {
        val target = snapshot(nodeId, seq)
        val current = nodes.find(nodeId) ?: throw ResourceNotFound("node $nodeId not found")
        val now = clock.instant()

        if (!nodes.replace(target.node.copy(version = current.version + 1, updatedAt = now), current.version)) {
            throw ResourceConflict("node $nodeId changed during rollback")
        }
        restoreBoards(nodeId, target.boards, now)
        restorePlacements(nodeId, target.placements)

        publisher.publish(setOf(nodeId), "node.rolled-back", actor).single()
    }

    private fun restoreBoards(nodeId: String, target: List<Board>, now: java.time.Instant) {
        val existing = boards.listByNode(nodeId).associateBy(Board::id)
        target.forEach { board ->
            val current = existing[board.id]
            if (current == null) {
                boards.create(board.copy(version = 1, updatedAt = now))
            } else if (!boards.replace(board.copy(version = current.version + 1, updatedAt = now), current.version)) {
                throw ResourceConflict("board ${board.id} changed during rollback")
            }
        }
        (existing.keys - target.map(Board::id).toSet()).forEach { boards.delete(nodeId, it) }
    }

    /**
     * Пользователь мог быть удалён из флота после снимка — его размещение молча
     * пропускается: воскрешать людей откат конфигурации ноды не должен.
     */
    private fun restorePlacements(nodeId: String, placements: List<UserPlacement>) {
        val restorable = placements
            .filter { users.find(it.user.id) != null }
            .associate { it.user.id to it.boardIds }
        grants.replaceOnNode(nodeId, restorable)
    }

    private fun snapshot(nodeId: String, seq: Long): NodeState =
        snapshots.find(nodeId, seq) ?: throw ResourceNotFound("snapshot $seq of node $nodeId not found")

    /** Плоское представление снимка для сравнения. Секретов здесь нет намеренно. */
    private fun flatten(state: NodeState): Map<String, String> = buildMap {
        put("node.name", state.node.name)
        put("node.state", state.node.state.name.lowercase())
        state.node.core.let { core ->
            put("node.idleTimeout", core.server.idleTimeout.toString())
            put("node.allowPrivateEgress", core.server.allowPrivateEgress.toString())
            put("node.transport.window", core.transport.window.toString())
            put("node.transport.maxFramePayload", core.transport.maxFramePayload.toString())
            put("node.transport.streamWindow", core.transport.streamWindow.toString())
            put("node.transport.maxStreamWindow", core.transport.maxStreamWindow.toString())
            put("node.transport.ackTimeout", core.transport.ackTimeout.toString())
            put("node.transport.coalesceTarget", core.transport.coalesceTarget.toString())
            put("node.transport.streamIdleTimeout", core.transport.streamIdleTimeout.toString())
            put("node.management.grpcListen", core.management.grpcListen)
            core.management.httpListen?.let { put("node.management.httpListen", it) }
            put("node.observability.enabled", core.observability.enabled.toString())
            put("node.observability.logLevel", core.observability.logLevel)
        }
        state.boards.sortedBy(Board::id).forEach { board ->
            put("boards.${board.id}.name", board.name)
            put("boards.${board.id}.hash", board.hash)
            put("boards.${board.id}.state", board.state.state())
            put("boards.${board.id}.maxLanes", board.maxLanes.toString())
            board.hubSlide?.let { put("boards.${board.id}.hubSlide", it) }
            board.apiBase?.let { put("boards.${board.id}.apiBase", it) }
            board.guestName?.let { put("boards.${board.id}.guestName", it) }
        }
        state.placements.sortedBy { it.user.id }.forEach { placement ->
            val id = placement.user.id
            put("users.$id.name", placement.user.name)
            put("users.$id.state", placement.user.state.state())
            put("users.$id.maxSessions", placement.user.maxSessions.toString())
            put("users.$id.maxLanes", placement.user.maxLanes.toString())
            put("users.$id.boards", placement.boardIds.sorted().joinToString(","))
        }
    }
}

private fun io.boardproxy.control.provisioning.domain.model.ResourceState.state() = name.lowercase()
