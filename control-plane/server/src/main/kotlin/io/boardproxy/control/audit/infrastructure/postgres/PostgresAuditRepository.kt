package io.boardproxy.control.audit.infrastructure.postgres

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.audit.application.AuditRepository
import io.boardproxy.control.audit.domain.AuditEvent
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository

@Repository
class PostgresAuditRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val json: ObjectMapper,
) : AuditRepository {
    override fun append(event: AuditEvent) {
        jdbc.update(
            """
            INSERT INTO audit_events (
                event_id, node_id, actor, action, resource_type, resource_id,
                resource_version, catalog_version, details, occurred_at
            ) VALUES (
                :id, :nodeId, :actor, :action, :resourceType, :resourceId,
                :resourceVersion, :catalogVersion, CAST(:details AS jsonb), :occurredAt
            )
            """.trimIndent(),
            mapOf(
                "id" to event.id, "nodeId" to event.nodeId, "actor" to event.actor,
                "action" to event.action, "resourceType" to event.resourceType,
                "resourceId" to event.resourceId, "resourceVersion" to event.resourceVersion,
                "catalogVersion" to event.catalogVersion,
                "details" to json.writeValueAsString(event.details),
                "occurredAt" to event.occurredAt.toSqlTimestamp(),
            ),
        )
    }
}
