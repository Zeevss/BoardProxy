package io.boardproxy.control.runtime.infrastructure.persistence.postgres

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.runtime.application.RuntimeEventView
import io.boardproxy.control.runtime.application.RuntimeQueries
import io.boardproxy.control.runtime.application.RuntimeSnapshotView
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository

@Repository
class PostgresRuntimeQueries(
    private val jdbc: NamedParameterJdbcTemplate,
    private val json: ObjectMapper,
) : RuntimeQueries {

    @Suppress("UNCHECKED_CAST")
    override fun snapshot(nodeId: String): RuntimeSnapshotView? = jdbc.query(
        "SELECT node_id, snapshot::text AS snapshot, observed_at FROM node_runtime WHERE node_id = :nodeId",
        mapOf("nodeId" to nodeId),
    ) { rs, _ ->
        RuntimeSnapshotView(
            nodeId = rs.getString("node_id"),
            snapshot = json.readValue(rs.getString("snapshot"), Map::class.java) as Map<String, Any?>,
            observedAt = rs.getTimestamp("observed_at").toInstant(),
        )
    }.firstOrNull()

    @Suppress("UNCHECKED_CAST")
    override fun events(nodeId: String, offset: Int, limit: Int): List<RuntimeEventView> = jdbc.query(
        """
        SELECT id, event_type, payload::text AS payload, occurred_at, received_at
        FROM runtime_events WHERE node_id = :nodeId
        ORDER BY occurred_at DESC, id DESC OFFSET :offset LIMIT :limit
        """.trimIndent(),
        mapOf("nodeId" to nodeId, "offset" to offset, "limit" to limit),
    ) { rs, _ ->
        RuntimeEventView(
            id = rs.getLong("id"),
            type = rs.getString("event_type"),
            payload = json.readValue(rs.getString("payload"), Map::class.java) as Map<String, Any?>,
            occurredAt = rs.getTimestamp("occurred_at").toInstant(),
            receivedAt = rs.getTimestamp("received_at").toInstant(),
        )
    }

    override fun countEvents(nodeId: String): Long = jdbc.queryForObject(
        "SELECT count(*) FROM runtime_events WHERE node_id = :nodeId",
        mapOf("nodeId" to nodeId),
        Long::class.java,
    ) ?: 0
}
