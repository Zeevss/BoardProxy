package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.AssignedUser
import io.boardproxy.control.provisioning.domain.model.Board
import io.boardproxy.control.provisioning.domain.model.Catalog
import io.boardproxy.control.provisioning.domain.model.NodeAssignment
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.provisioning.domain.model.User
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceConflict
import java.time.Clock

data class BoardInput(
    val name: String,
    val hash: String,
    val hubSlide: String?,
    val apiBase: String?,
    val guestName: String?,
    val state: ResourceState,
    val maxLanes: Int,
)

data class UserInput(
    val name: String,
    val privateKey: String?,
    val publicKey: String?,
    val state: ResourceState,
    val maxSessions: Int,
    val maxLanes: Int,
)

data class NodeInput(val name: String?, val state: ResourceState?)

interface CatalogResourceCommands {
    fun updateNode(nodeId: String, expectedVersion: Long, input: NodeInput, actor: String): CatalogMutationResult
    fun putBoard(
        nodeId: String,
        boardId: String,
        expectedVersion: Long,
        input: BoardInput,
        actor: String,
    ): CatalogMutationResult
    fun removeBoard(nodeId: String, boardId: String, expectedVersion: Long, actor: String): CatalogMutationResult
    fun putUser(
        nodeId: String,
        userId: String,
        expectedVersion: Long,
        input: UserInput,
        actor: String,
    ): CatalogMutationResult
    fun removeUser(nodeId: String, userId: String, expectedVersion: Long, actor: String): CatalogMutationResult
    fun replaceAssignment(
        nodeId: String,
        expectedVersion: Long,
        boardIds: List<String>,
        users: List<AssignedUser>,
        actor: String,
    ): CatalogMutationResult
}

class CatalogResourceService(
    private val queries: CatalogQueries,
    private val commands: CatalogCommands,
    private val clock: Clock,
) : CatalogResourceCommands {
    override fun updateNode(
        nodeId: String,
        expectedVersion: Long,
        input: NodeInput,
        actor: String,
    ): CatalogMutationResult = mutate(nodeId, expectedVersion, actor, "node.updated") { current, now ->
        current.copy(
            node = current.node.copy(
                name = input.name ?: current.node.name,
                state = input.state ?: current.node.state,
                version = current.node.version + 1,
                updatedAt = now,
            ),
            version = current.version + 1,
            updatedAt = now,
        )
    }

    override fun putBoard(
        nodeId: String,
        boardId: String,
        expectedVersion: Long,
        input: BoardInput,
        actor: String,
    ): CatalogMutationResult = mutate(nodeId, expectedVersion, actor, "board.updated") { current, now ->
        val previous = current.boards.firstOrNull { it.id == boardId }
        val board = Board(
            boardId, input.name, input.hash, input.hubSlide, input.apiBase, input.guestName,
            input.state, input.maxLanes, previous?.version?.plus(1) ?: 1, now,
        )
        current.copy(
            boards = current.boards.filterNot { it.id == boardId } + board,
            version = current.version + 1,
            updatedAt = now,
        )
    }

    override fun removeBoard(
        nodeId: String,
        boardId: String,
        expectedVersion: Long,
        actor: String,
    ): CatalogMutationResult = mutate(nodeId, expectedVersion, actor, "board.removed") { current, now ->
        if (current.boards.none { it.id == boardId }) throw InvalidRequest("board $boardId does not exist")
        if (boardId in current.assignment.boardIds) {
            throw ResourceConflict("board $boardId is assigned; update assignment before removal")
        }
        current.copy(boards = current.boards.filterNot { it.id == boardId }, version = current.version + 1, updatedAt = now)
    }

    override fun putUser(
        nodeId: String,
        userId: String,
        expectedVersion: Long,
        input: UserInput,
        actor: String,
    ): CatalogMutationResult = mutate(nodeId, expectedVersion, actor, "user.updated") { current, now ->
        val previous = current.users.firstOrNull { it.id == userId }
        val privateKey = input.privateKey ?: previous?.privateKey
        val publicKey = input.publicKey ?: if (input.privateKey == null) previous?.publicKey else null
        val user = User(
            userId, input.name, privateKey, publicKey, input.state, input.maxSessions, input.maxLanes,
            previous?.version?.plus(1) ?: 1, now,
        )
        current.copy(
            users = current.users.filterNot { it.id == userId } + user,
            version = current.version + 1,
            updatedAt = now,
        )
    }

    override fun removeUser(
        nodeId: String,
        userId: String,
        expectedVersion: Long,
        actor: String,
    ): CatalogMutationResult = mutate(nodeId, expectedVersion, actor, "user.removed") { current, now ->
        if (current.users.none { it.id == userId }) throw InvalidRequest("user $userId does not exist")
        if (current.assignment.users.any { it.userId == userId }) {
            throw ResourceConflict("user $userId is assigned; update assignment before removal")
        }
        current.copy(users = current.users.filterNot { it.id == userId }, version = current.version + 1, updatedAt = now)
    }

    override fun replaceAssignment(
        nodeId: String,
        expectedVersion: Long,
        boardIds: List<String>,
        users: List<AssignedUser>,
        actor: String,
    ): CatalogMutationResult = mutate(nodeId, expectedVersion, actor, "assignment.replaced") { current, now ->
        current.copy(
            assignment = NodeAssignment(nodeId, boardIds, users, current.assignment.version + 1, now),
            version = current.version + 1,
            updatedAt = now,
        )
    }

    private fun mutate(
        nodeId: String,
        expectedVersion: Long,
        actor: String,
        cause: String,
        transform: (Catalog, java.time.Instant) -> Catalog,
    ): CatalogMutationResult {
        val current = queries.get(nodeId)
        if (current.version != expectedVersion) throw ResourceConflict("catalog $nodeId version changed")
        return commands.replace(transform(current, clock.instant()), expectedVersion, actor, cause)
    }
}
