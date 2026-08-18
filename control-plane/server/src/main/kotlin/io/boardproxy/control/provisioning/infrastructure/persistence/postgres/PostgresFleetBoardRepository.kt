package io.boardproxy.control.provisioning.infrastructure.persistence.postgres

import io.boardproxy.control.provisioning.application.FleetBoard
import io.boardproxy.control.provisioning.application.FleetBoardQueries
import io.boardproxy.control.provisioning.domain.model.ResourceState
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet

/** Борды всего флота одним запросом; группировку по нодам делает панель. */
@Repository
class PostgresFleetBoardRepository(private val jdbc: NamedParameterJdbcTemplate) : FleetBoardQueries {
    override fun list(query: String?): List<FleetBoard> = jdbc.query(
        SELECT,
        mapOf("query" to query?.trim()?.takeUnless(String::isEmpty)?.lowercase()?.let { "%$it%" }),
    ) { rs, _ -> row(rs) }

    private fun row(rs: ResultSet) = FleetBoard(
        nodeId = rs.getString("node_id"),
        nodeName = rs.getString("node_name"),
        nodeState = ResourceState.valueOf(rs.getString("node_state").uppercase()),
        id = rs.getString("board_id"),
        name = rs.getString("board_name"),
        hash = rs.getString("board_hash"),
        hubSlide = rs.getString("hub_slide"),
        apiBase = rs.getString("api_base"),
        guestName = rs.getString("guest_name"),
        state = ResourceState.valueOf(rs.getString("board_state").uppercase()),
        maxLanes = rs.getInt("max_lanes"),
        assigned = rs.getBoolean("assigned"),
        users = rs.getInt("users"),
        version = rs.getLong("resource_version"),
        updatedAt = rs.getTimestamp("updated_at").toInstant(),
    )

    private companion object {
        const val SELECT = """
            SELECT n.id AS node_id, n.name AS node_name, n.state AS node_state,
                   b.id AS board_id, b.name AS board_name, b.board_hash, b.state AS board_state,
                   b.hub_slide, b.api_base, b.guest_name,
                   b.max_lanes, b.resource_version, b.updated_at,
                   (nb.board_id IS NOT NULL) AS assigned,
                   (SELECT count(*) FROM node_user_boards nub
                     WHERE nub.node_id = b.node_id AND nub.board_id = b.id) AS users
            FROM boards b
            JOIN nodes n ON n.id = b.node_id
            LEFT JOIN node_boards nb ON nb.node_id = b.node_id AND nb.board_id = b.id
            WHERE (CAST(:query AS text) IS NULL
                   OR lower(b.name) LIKE CAST(:query AS text)
                   OR lower(b.id) LIKE CAST(:query AS text)
                   OR lower(n.name) LIKE CAST(:query AS text))
            ORDER BY n.name, b.name
        """
    }
}
