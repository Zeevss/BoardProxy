package io.boardproxy.control.provisioning.infrastructure.persistence.postgres

import io.boardproxy.control.provisioning.application.BoardRepository
import io.boardproxy.control.provisioning.domain.model.Board
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository

private const val COLUMNS =
    """node_id, id, name, board_hash, hub_slide, api_base, guest_name, state,
       max_lanes, resource_version, updated_at"""

@Repository
class PostgresBoardRepository(
    private val jdbc: NamedParameterJdbcTemplate,
) : BoardRepository {

    override fun find(nodeId: String, id: String): Board? = jdbc.query(
        "SELECT $COLUMNS FROM boards WHERE node_id = :nodeId AND id = :id",
        mapOf("nodeId" to nodeId, "id" to id),
    ) { rs, _ -> boardRow(rs) }.firstOrNull()

    /**
     * Панель адресует борд одним идентификатором, хотя ключ составной. Пока id
     * уникален во флоте, это удобно; коллизия по разным нодам вернёт первый —
     * поэтому вызывающий, знающий ноду, обязан пользоваться [find].
     */
    override fun findById(id: String): Board? = jdbc.query(
        "SELECT $COLUMNS FROM boards WHERE id = :id ORDER BY node_id LIMIT 1",
        mapOf("id" to id),
    ) { rs, _ -> boardRow(rs) }.firstOrNull()

    override fun listByNode(nodeId: String): List<Board> = jdbc.query(
        "SELECT $COLUMNS FROM boards WHERE node_id = :nodeId ORDER BY id",
        mapOf("nodeId" to nodeId),
    ) { rs, _ -> boardRow(rs) }

    override fun list(query: String?, nodeId: String?, offset: Int, limit: Int): List<Board> = jdbc.query(
        """
        SELECT $COLUMNS FROM boards
        ${where(query, nodeId)}
        ORDER BY node_id, id OFFSET :offset LIMIT :limit
        """.trimIndent(),
        filterParameters(query, nodeId) + mapOf("offset" to offset, "limit" to limit),
    ) { rs, _ -> boardRow(rs) }

    override fun count(query: String?, nodeId: String?): Long = jdbc.queryForObject(
        "SELECT count(*) FROM boards ${where(query, nodeId)}",
        filterParameters(query, nodeId),
        Long::class.java,
    ) ?: 0

    override fun create(board: Board) {
        jdbc.update(
            """
            INSERT INTO boards (
                node_id, id, name, board_hash, hub_slide, api_base, guest_name,
                state, max_lanes, resource_version, updated_at
            ) VALUES (
                :nodeId, :id, :name, :hash, :hubSlide, :apiBase, :guestName,
                :state, :maxLanes, :version, :updatedAt
            )
            """.trimIndent(),
            parameters(board),
        )
    }

    override fun replace(board: Board, expectedVersion: Long): Boolean = jdbc.update(
        """
        UPDATE boards SET
            name = :name, board_hash = :hash, hub_slide = :hubSlide, api_base = :apiBase,
            guest_name = :guestName, state = :state, max_lanes = :maxLanes,
            resource_version = :version, updated_at = :updatedAt
        WHERE node_id = :nodeId AND id = :id AND resource_version = :expectedVersion
        """.trimIndent(),
        parameters(board) + ("expectedVersion" to expectedVersion),
    ) == 1

    /** Гранты на этот борд уходят каскадом: доступа к удалённому борду не бывает. */
    override fun delete(nodeId: String, id: String): Boolean = jdbc.update(
        "DELETE FROM boards WHERE node_id = :nodeId AND id = :id",
        mapOf("nodeId" to nodeId, "id" to id),
    ) == 1

    private fun parameters(board: Board): Map<String, Any?> = mapOf(
        "nodeId" to board.nodeId,
        "id" to board.id,
        "name" to board.name,
        "hash" to board.hash,
        "hubSlide" to board.hubSlide,
        "apiBase" to board.apiBase,
        "guestName" to board.guestName,
        "state" to board.state.databaseValue(),
        "maxLanes" to board.maxLanes,
        "version" to board.version,
        "updatedAt" to board.updatedAt.toSqlTimestamp(),
    )

    private fun where(query: String?, nodeId: String?): String {
        val conditions = buildList {
            if (!nodeId.isNullOrBlank()) add("node_id = :nodeId")
            if (!query.isNullOrBlank()) add("(id ILIKE '%' || :query || '%' OR name ILIKE '%' || :query || '%' OR board_hash ILIKE '%' || :query || '%')")
        }
        return if (conditions.isEmpty()) "" else "WHERE ${conditions.joinToString(" AND ")}"
    }

    private fun filterParameters(query: String?, nodeId: String?): Map<String, Any?> =
        mapOf("query" to query.orEmpty(), "nodeId" to nodeId.orEmpty())
}
