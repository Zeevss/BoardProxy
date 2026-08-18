package io.boardproxy.control.subscriber.infrastructure.persistence.postgres

import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.subscriber.application.FleetUserRecord
import io.boardproxy.control.subscriber.application.FleetUserRepository
import io.boardproxy.control.subscriber.domain.UserBoard
import io.boardproxy.control.subscriber.domain.UserPlacement
import io.boardproxy.control.subscriber.domain.UserSubscription
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet
import java.time.Instant

/**
 * Пользователь хранится по одной строке на ноду, поэтому флотовая запись собирается
 * группировкой по `users.id`. Борды и подписка подтягиваются тем же запросом.
 */
@Repository
class PostgresFleetUserRepository(private val jdbc: NamedParameterJdbcTemplate) : FleetUserRepository {
    override fun list(query: String?): List<FleetUserRecord> {
        val rows = jdbc.query(
            SELECT,
            mapOf("query" to query?.lowercase()?.let { "%$it%" }),
        ) { rs, _ -> row(rs) }
        return rows
            .groupBy(Row::userId)
            .map { (userId, group) -> record(userId, group) }
            .sortedBy { it.name.lowercase() }
    }

    private fun record(userId: String, group: List<Row>): FleetUserRecord {
        val newest = group.maxBy(Row::updatedAt)
        val placements = group
            .groupBy(Row::nodeId)
            .map { (nodeId, perNode) ->
                val head = perNode.first()
                UserPlacement(
                    nodeId = nodeId,
                    nodeName = head.nodeName,
                    state = ResourceState.valueOf(head.state.uppercase()),
                    boards = perNode.mapNotNull { it.boardId?.let { id -> UserBoard(id, it.boardName ?: id) } }
                        .distinctBy(UserBoard::id)
                        .sortedBy(UserBoard::name),
                    version = head.version,
                )
            }
            .sortedBy(UserPlacement::nodeName)
        return FleetUserRecord(
            id = userId,
            name = newest.name,
            // Пользователь считается отозванным/выключенным только когда это верно везде,
            // иначе панель показала бы доступ закрытым при живом доступе на другой ноде.
            state = placements.map(UserPlacement::state).reduce(::widest),
            placements = placements,
            maxDevices = newest.maxSessions,
            maxPages = newest.maxLanes,
            subscription = newest.subscriptionId?.let {
                UserSubscription(it, newest.subscriptionName ?: it, newest.subscriptionState ?: "unknown")
            },
            updatedAt = newest.updatedAt,
        )
    }

    private fun widest(left: ResourceState, right: ResourceState) =
        listOf(left, right).minBy { PRECEDENCE.indexOf(it) }

    private fun row(rs: ResultSet) = Row(
        userId = rs.getString("user_id"),
        name = rs.getString("user_name"),
        state = rs.getString("user_state"),
        maxSessions = rs.getInt("max_sessions"),
        maxLanes = rs.getInt("max_lanes"),
        version = rs.getLong("resource_version"),
        updatedAt = rs.getTimestamp("updated_at").toInstant(),
        nodeId = rs.getString("node_id"),
        nodeName = rs.getString("node_name"),
        boardId = rs.getString("board_id"),
        boardName = rs.getString("board_name"),
        subscriptionId = rs.getString("subscription_id"),
        subscriptionName = rs.getString("subscription_name"),
        subscriptionState = rs.getString("subscription_state"),
    )

    private data class Row(
        val userId: String,
        val name: String,
        val state: String,
        val maxSessions: Int,
        val maxLanes: Int,
        val version: Long,
        val updatedAt: Instant,
        val nodeId: String,
        val nodeName: String,
        val boardId: String?,
        val boardName: String?,
        val subscriptionId: String?,
        val subscriptionName: String?,
        val subscriptionState: String?,
    )

    private companion object {
        /** От самого «открытого» состояния к самому строгому: побеждает открытое. */
        val PRECEDENCE = listOf(ResourceState.ENABLED, ResourceState.DISABLED, ResourceState.REVOKED)

        const val SELECT = """
            SELECT u.id AS user_id, u.name AS user_name, u.state AS user_state,
                   u.max_sessions, u.max_lanes, u.resource_version, u.updated_at,
                   n.id AS node_id, n.name AS node_name,
                   b.id AS board_id, b.name AS board_name,
                   s.id AS subscription_id, s.name AS subscription_name, s.state AS subscription_state
            FROM users u
            JOIN nodes n ON n.id = u.node_id
            LEFT JOIN node_user_boards nub ON nub.node_id = u.node_id AND nub.user_id = u.id
            LEFT JOIN boards b ON b.node_id = nub.node_id AND b.id = nub.board_id
            LEFT JOIN subscription_keys sk ON sk.node_id = u.node_id AND sk.user_id = u.id
            LEFT JOIN subscriptions s ON s.id = sk.subscription_id
            -- Приведение обязательно: без него PostgreSQL не может вывести тип
            -- нетипизированного NULL-параметра и запрос падает на пустом поиске.
            WHERE (CAST(:query AS text) IS NULL
                   OR lower(u.name) LIKE CAST(:query AS text)
                   OR lower(u.id) LIKE CAST(:query AS text))
            ORDER BY u.id, n.name, b.name
        """
    }
}
