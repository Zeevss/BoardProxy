package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.Board
import io.boardproxy.control.provisioning.domain.model.Grant
import io.boardproxy.control.provisioning.domain.model.Node
import io.boardproxy.control.provisioning.domain.model.NodeState
import io.boardproxy.control.provisioning.domain.model.User
import java.time.Instant

/**
 * Репозитории сущностей. Оптимистичная блокировка теперь по версии конкретной
 * сущности, а не по версии ноды: два оператора, правящие разных пользователей
 * одной ноды, друг другу не мешают.
 */

data class Page<T>(val items: List<T>, val offset: Int, val limit: Int, val total: Long)

interface NodeRepository {
    fun find(id: String): Node?
    fun list(query: String?, offset: Int, limit: Int): List<Node>
    fun count(query: String?): Long
    fun create(node: Node)
    fun replace(node: Node, expectedVersion: Long): Boolean
    fun delete(id: String): Boolean
}

interface BoardRepository {
    fun find(nodeId: String, id: String): Board?
    fun findById(id: String): Board?
    fun listByNode(nodeId: String): List<Board>
    fun list(query: String?, nodeId: String?, offset: Int, limit: Int): List<Board>
    fun count(query: String?, nodeId: String?): Long
    fun create(board: Board)
    fun replace(board: Board, expectedVersion: Long): Boolean
    fun delete(nodeId: String, id: String): Boolean
}

interface UserRepository {
    fun find(id: String): User?
    fun findByFingerprint(fingerprint: String): User?
    fun list(query: String?, nodeId: String?, offset: Int, limit: Int): List<User>
    fun count(query: String?, nodeId: String?): Long
    fun create(user: User)
    fun replace(user: User, expectedVersion: Long): Boolean
    fun delete(id: String): Boolean
}

interface GrantRepository {
    fun of(userId: String): List<Grant>
    fun onNode(nodeId: String): List<Grant>
    /** Заменяет размещения пользователя целиком: частичные правки порождали рассинхрон. */
    fun replace(userId: String, grants: List<Grant>)

    /**
     * Заменяет размещения на одной ноде, не трогая гранты этих же пользователей
     * на других. Нужно откату: он восстанавливает состояние ноды, а не флота.
     */
    fun replaceOnNode(nodeId: String, placements: Map<String, Set<String>>)

    fun nodesOf(userId: String): Set<String>

    /**
     * Ноды сразу для набора пользователей: список показывает размещение в каждой
     * строке, а [nodesOf] на страницу из полусотни человек означала бы полусотню
     * запросов ради одной таблицы.
     */
    fun nodesOfAll(userIds: Collection<String>): Map<String, Set<String>>
}

/** Текущая конфигурация ноды. История — в [NodeSnapshotRepository]. */
data class DesiredConfig(
    val nodeId: String,
    val revision: Long,
    val configSha256: String,
    val configToml: ByteArray,
    val updatedAt: Instant,
) {
    override fun equals(other: Any?): Boolean = other is DesiredConfig &&
        nodeId == other.nodeId && revision == other.revision && configSha256 == other.configSha256

    override fun hashCode(): Int = 31 * nodeId.hashCode() + revision.hashCode()
}

interface DesiredConfigRepository {
    fun find(nodeId: String): DesiredConfig?
    fun save(config: DesiredConfig)
}

data class NodeSnapshotMetadata(
    val nodeId: String,
    val seq: Long,
    val cause: String,
    val actor: String,
    val createdAt: Instant,
)

interface NodeSnapshotRepository {
    fun save(state: NodeState, cause: String, actor: String, at: Instant): Long
    fun find(nodeId: String, seq: Long): NodeState?
    fun list(nodeId: String, offset: Int, limit: Int): List<NodeSnapshotMetadata>
    fun count(nodeId: String): Long
}
