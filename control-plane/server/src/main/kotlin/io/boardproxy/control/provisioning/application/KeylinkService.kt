package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.UserOnNode
import io.boardproxy.control.provisioning.domain.model.keylinkFor
import io.boardproxy.control.shared.contracts.KeylinkQueries
import io.boardproxy.control.shared.contracts.NodeKeylink
import io.boardproxy.control.shared.contracts.QuotaExceededQueries

/**
 * Собирает ключи пользователя по всем нодам, где он размещён.
 *
 * Ссылка выводится из ключей и никогда не хранится, поэтому отзыв пользователя,
 * борда или исчерпание квоты действуют немедленно — без правки подписки.
 */
class KeylinkService(
    private val nodes: NodeRepository,
    private val boards: BoardRepository,
    private val users: UserRepository,
    private val grants: GrantRepository,
    private val quotas: QuotaExceededQueries,
) : KeylinkQueries {

    override fun forUser(userId: String, label: String): List<NodeKeylink> {
        val user = users.find(userId) ?: return emptyList()
        val exceeded = userId in quotas.exceededUsers()

        return grants.of(userId).mapNotNull { grant ->
            val node = nodes.find(grant.nodeId) ?: return@mapNotNull null
            val placement = UserOnNode(user, grant.boardIds, quotaExceeded = exceeded)
            NodeKeylink(
                nodeId = node.id,
                nodeName = node.name,
                keylink = keylinkFor(node, placement, boards.listByNode(node.id), label),
            )
        }.sortedBy(NodeKeylink::nodeId)
    }
}
