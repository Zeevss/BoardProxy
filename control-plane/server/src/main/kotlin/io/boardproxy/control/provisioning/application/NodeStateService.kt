package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.Board
import io.boardproxy.control.provisioning.domain.model.NodeState
import io.boardproxy.control.provisioning.domain.model.UserPlacement
import io.boardproxy.control.shared.errors.ResourceNotFound

/**
 * Собирает владеемое состояние ноды из нормализованных таблиц.
 *
 * Это read-model, а не агрегат: сборка не валидирует набор целиком, потому что
 * ссылочную целостность держат внешние ключи. Прежний Catalog перепроверял всё
 * на каждую правку одного поля — ровно то, от чего мы уходим.
 */
fun interface NodeStateLoader {
    fun load(nodeId: String): NodeState
}

class NodeStateService(
    private val nodes: NodeRepository,
    private val boards: BoardRepository,
    private val users: UserRepository,
    private val grants: GrantRepository,
) : NodeStateLoader {
    override fun load(nodeId: String): NodeState {
        val node = nodes.find(nodeId) ?: throw ResourceNotFound("node $nodeId not found")
        val nodeBoards = boards.listByNode(nodeId)
        val boardIds = nodeBoards.map(Board::id).toSet()

        val placements = grants.onNode(nodeId)
            .mapNotNull { grant ->
                val user = users.find(grant.userId) ?: return@mapNotNull null
                val granted = grant.boardIds.filter(boardIds::contains).toSet()
                if (granted.isEmpty()) null else UserPlacement(user, granted)
            }
            .sortedBy { it.user.id }

        return NodeState(node, nodeBoards.sortedBy(Board::id), placements)
    }
}
