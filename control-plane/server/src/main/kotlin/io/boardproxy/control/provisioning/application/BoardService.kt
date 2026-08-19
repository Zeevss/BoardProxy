package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.Board
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.errors.ResourceNotFound
import io.boardproxy.control.shared.persistence.TransactionRunner
import java.time.Clock

data class BoardInput(
    val id: String? = null,
    val nodeId: String,
    val name: String,
    val hash: String,
    val hubSlide: String? = null,
    val apiBase: String? = null,
    val guestName: String? = null,
    val state: ResourceState = ResourceState.ENABLED,
    val maxLanes: Int = 4,
)

class BoardService(
    private val boards: BoardRepository,
    private val nodes: NodeRepository,
    private val publisher: DesiredConfigPublisher,
    private val transactions: TransactionRunner,
    private val clock: Clock,
) {
    fun get(nodeId: String, id: String): Board =
        boards.find(nodeId, id) ?: throw ResourceNotFound("board $id not found on node $nodeId")

    fun list(query: String?, nodeId: String?, offset: Int, limit: Int): Page<Board> =
        Page(boards.list(query, nodeId, offset, limit), offset, limit, boards.count(query, nodeId))

    fun create(input: BoardInput, actor: String): Board = transactions.required {
        val id = input.id?.trim().orEmpty().ifBlank { throw InvalidRequest("board id is required") }
        nodes.find(input.nodeId) ?: throw ResourceNotFound("node ${input.nodeId} not found")
        if (boards.find(input.nodeId, id) != null) {
            throw ResourceConflict("board $id already exists on node ${input.nodeId}")
        }
        val board = input.toBoard(id, version = 1)
        boards.create(board)
        publisher.publish(setOf(input.nodeId), "board.created", actor)
        board
    }

    fun update(nodeId: String, id: String, expectedVersion: Long, input: BoardInput, actor: String): Board =
        transactions.required {
            get(nodeId, id)
            val updated = input.copy(nodeId = nodeId).toBoard(id, version = expectedVersion + 1)
            if (!boards.replace(updated, expectedVersion)) throw ResourceConflict("board $id version changed")
            publisher.publish(setOf(nodeId), "board.updated", actor)
            updated
        }

    /** Гранты на этот борд уходят каскадом: доступа к удалённому борду не бывает. */
    fun delete(nodeId: String, id: String, expectedVersion: Long, actor: String) = transactions.required {
        val current = get(nodeId, id)
        if (current.version != expectedVersion) throw ResourceConflict("board $id version changed")
        if (!boards.delete(nodeId, id)) throw ResourceNotFound("board $id not found on node $nodeId")
        publisher.publish(setOf(nodeId), "board.deleted", actor)
    }

    private fun BoardInput.toBoard(id: String, version: Long) = Board(
        nodeId = nodeId,
        id = id,
        name = name.trim(),
        hash = hash.trim(),
        hubSlide = hubSlide?.trim()?.takeIf(String::isNotEmpty),
        apiBase = apiBase?.trim()?.takeIf(String::isNotEmpty),
        guestName = guestName?.trim()?.takeIf(String::isNotEmpty),
        state = state,
        maxLanes = maxLanes,
        version = version,
        updatedAt = clock.instant(),
    )
}
