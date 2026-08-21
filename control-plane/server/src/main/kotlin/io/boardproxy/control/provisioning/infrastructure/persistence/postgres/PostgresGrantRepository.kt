package io.boardproxy.control.provisioning.infrastructure.persistence.postgres

import io.boardproxy.control.provisioning.application.GrantRepository
import io.boardproxy.control.provisioning.domain.model.Grant
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository

/**
 * Гранты — единственное представление размещения. Строки плоские
 * (user, node, board), а доменный [Grant] группирует их по ноде.
 */
@Repository
class PostgresGrantRepository(
    private val jdbc: NamedParameterJdbcTemplate,
) : GrantRepository {

    override fun of(userId: String): List<Grant> = jdbc.query(
        "SELECT node_id, board_id FROM grants WHERE user_id = :userId ORDER BY node_id, board_id",
        mapOf("userId" to userId),
    ) { rs, _ -> rs.getString("node_id") to rs.getString("board_id") }
        .groupBy({ it.first }, { it.second })
        .map { (nodeId, boards) -> Grant(userId, nodeId, boards.toSet()) }

    override fun onNode(nodeId: String): List<Grant> = jdbc.query(
        "SELECT user_id, board_id FROM grants WHERE node_id = :nodeId ORDER BY user_id, board_id",
        mapOf("nodeId" to nodeId),
    ) { rs, _ -> rs.getString("user_id") to rs.getString("board_id") }
        .groupBy({ it.first }, { it.second })
        .map { (userId, boards) -> Grant(userId, nodeId, boards.toSet()) }

    /**
     * Замена целиком, а не точечные правки: частичное обновление размещений было
     * источником рассинхрона между тем, что видит панель, и тем, что уезжает
     * в конфигурацию.
     */
    override fun replace(userId: String, grants: List<Grant>) {
        jdbc.update("DELETE FROM grants WHERE user_id = :userId", mapOf("userId" to userId))
        grants.forEach { grant ->
            grant.boardIds.forEach { boardId ->
                jdbc.update(
                    "INSERT INTO grants (user_id, node_id, board_id) VALUES (:userId, :nodeId, :boardId)",
                    mapOf("userId" to userId, "nodeId" to grant.nodeId, "boardId" to boardId),
                )
            }
        }
    }

    override fun replaceOnNode(nodeId: String, placements: Map<String, Set<String>>) {
        jdbc.update("DELETE FROM grants WHERE node_id = :nodeId", mapOf("nodeId" to nodeId))
        placements.forEach { (userId, boardIds) ->
            boardIds.forEach { boardId ->
                jdbc.update(
                    "INSERT INTO grants (user_id, node_id, board_id) VALUES (:userId, :nodeId, :boardId)",
                    mapOf("userId" to userId, "nodeId" to nodeId, "boardId" to boardId),
                )
            }
        }
    }

    /** Ноды, чью конфигурацию задевает правка этого пользователя. */
    override fun nodesOf(userId: String): Set<String> = jdbc.queryForList(
        "SELECT DISTINCT node_id FROM grants WHERE user_id = :userId",
        mapOf("userId" to userId),
        String::class.java,
    ).toSet()

    override fun nodesOfAll(userIds: Collection<String>): Map<String, Set<String>> {
        if (userIds.isEmpty()) return emptyMap()
        return jdbc.query(
            "SELECT DISTINCT user_id, node_id FROM grants WHERE user_id IN (:userIds)",
            mapOf("userIds" to userIds),
        ) { rs, _ -> rs.getString("user_id") to rs.getString("node_id") }
            .groupBy({ it.first }, { it.second })
            .mapValues { (_, nodes) -> nodes.toSortedSet() }
    }
}
