package io.boardproxy.control.shared.audit.postgres

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.shared.audit.AuditPage
import io.boardproxy.control.shared.audit.AuditQueries
import io.boardproxy.control.shared.audit.AuditRepository
import io.boardproxy.control.shared.audit.AuditEvent
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet

@Repository
class PostgresAuditRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val json: ObjectMapper,
) : AuditRepository, AuditQueries {
    override fun append(event: AuditEvent) {
        jdbc.update(
            """
            INSERT INTO audit_events (
                event_id, node_id, actor, action, resource_type, resource_id,
                resource_version, details, occurred_at
            ) VALUES (
                :id, :nodeId, :actor, :action, :resourceType, :resourceId,
                :resourceVersion, CAST(:details AS jsonb), :occurredAt
            )
            """.trimIndent(),
            mapOf(
                "id" to event.id, "nodeId" to event.nodeId, "actor" to event.actor,
                "action" to event.action, "resourceType" to event.resourceType,
                "resourceId" to event.resourceId, "resourceVersion" to event.resourceVersion,
                "details" to json.writeValueAsString(event.details),
                "occurredAt" to event.occurredAt.toSqlTimestamp(),
            ),
        )
    }

    /**
     * Свежие записи первыми: лента активности читается сверху вниз, а глубже
     * первой страницы почти никто не уходит.
     */
    override fun list(nodeId: String?, offset: Int, limit: Int): AuditPage {
        val filter = if (nodeId == null) "" else "WHERE node_id = :nodeId"
        val parameters = mapOf("nodeId" to nodeId, "offset" to offset, "limit" to limit)

        val total = jdbc.queryForObject(
            "SELECT COUNT(*) FROM audit_events $filter",
            parameters,
            Long::class.java,
        ) ?: 0

        val items = jdbc.query(
            """
            SELECT event_id, node_id, actor, action, resource_type, resource_id,
                   resource_version, details::text AS details, occurred_at
            FROM audit_events $filter
            ORDER BY occurred_at DESC, event_id DESC
            OFFSET :offset LIMIT :limit
            """.trimIndent(),
            parameters,
        ) { rs, _ -> rs.toAuditEvent() }

        return AuditPage(items, offset, limit, total)
    }

    @Suppress("UNCHECKED_CAST")
    private fun ResultSet.toAuditEvent() = AuditEvent(
        id = getString("event_id"),
        nodeId = getString("node_id"),
        actor = getString("actor"),
        action = getString("action"),
        resourceType = getString("resource_type"),
        resourceId = getString("resource_id"),
        resourceVersion = getLong("resource_version"),
        details = json.readValue(getString("details"), Map::class.java) as Map<String, Any>,
        occurredAt = getTimestamp("occurred_at").toInstant(),
    )
}
