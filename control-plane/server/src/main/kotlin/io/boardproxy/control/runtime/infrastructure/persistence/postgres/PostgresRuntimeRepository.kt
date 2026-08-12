package io.boardproxy.control.runtime.infrastructure.persistence.postgres

import com.fasterxml.jackson.core.type.TypeReference
import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.runtime.application.RuntimeEventBatch
import io.boardproxy.control.runtime.application.RuntimeEventStore
import io.boardproxy.control.runtime.application.RuntimeEventView
import io.boardproxy.control.runtime.application.RuntimeQueries
import io.boardproxy.control.runtime.application.RuntimeReplayMaterial
import io.boardproxy.control.runtime.application.RuntimeReplayStore
import io.boardproxy.control.runtime.domain.RuntimeBoardState
import io.boardproxy.control.runtime.domain.RuntimeEvent
import io.boardproxy.control.runtime.domain.RuntimeEventPayload
import io.boardproxy.control.runtime.domain.RuntimeProjection
import io.boardproxy.control.runtime.domain.RuntimeSessionState
import io.boardproxy.control.runtime.domain.RuntimeSnapshot
import io.boardproxy.control.runtime.domain.RuntimeResourceKind
import io.boardproxy.control.runtime.domain.RuntimeResourceOperation
import io.boardproxy.control.runtime.domain.RuntimeUserState
import io.boardproxy.control.runtime.domain.type
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet

@Repository
class PostgresRuntimeRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val json: ObjectMapper,
) : RuntimeEventStore, RuntimeQueries, RuntimeReplayStore {
    override fun claimBatch(batch: RuntimeEventBatch): Boolean = jdbc.update(
        """
        INSERT INTO runtime_event_batches (node_id, batch_id, payload, snapshot)
        VALUES (:nodeId, :batchId, :payload, CAST(:snapshot AS jsonb))
        ON CONFLICT (node_id, batch_id) DO NOTHING
        """.trimIndent(),
        mapOf(
            "nodeId" to batch.nodeId,
            "batchId" to batch.batchId,
            "payload" to batch.rawPayload,
            "snapshot" to batch.snapshot?.let(json::writeValueAsString),
        ),
    ) == 1

    override fun appendEvent(nodeId: String, event: RuntimeEvent): Boolean = jdbc.update(
        """
        INSERT INTO runtime_events (
            event_id, node_id, core_boot_id, sequence_number, runtime_revision,
            event_type, payload, occurred_at
        ) VALUES (
            :eventId, :nodeId, :coreBootId, :sequence, :runtimeRevision,
            :eventType, CAST(:payload AS jsonb), :occurredAt
        )
        ON CONFLICT (node_id, event_id) DO NOTHING
        """.trimIndent(),
        mapOf(
            "eventId" to event.eventId,
            "nodeId" to nodeId,
            "coreBootId" to event.coreBootId,
            "sequence" to event.sequence,
            "runtimeRevision" to event.runtimeRevision,
            "eventType" to event.type(),
            "payload" to json.writeValueAsString(eventPayload(event.payload)),
            "occurredAt" to event.occurredAt.toSqlTimestamp(),
        ),
    ) == 1

    override fun lockProjection(nodeId: String): RuntimeProjection {
        jdbc.update(
            """
            INSERT INTO node_runtime_projection (node_id, projection_version)
            VALUES (:nodeId, 0)
            ON CONFLICT (node_id) DO NOTHING
            """.trimIndent(),
            mapOf("nodeId" to nodeId),
        )
        return requireNotNull(
            jdbc.query(
                "$PROJECTION_SELECT WHERE node_id = :nodeId FOR UPDATE",
                mapOf("nodeId" to nodeId),
            ) { rs, _ -> mapProjection(rs) }.firstOrNull(),
        ) { "runtime projection row disappeared for node $nodeId" }
    }

    override fun saveProjection(projection: RuntimeProjection) {
        val updated = jdbc.update(
            """
            UPDATE node_runtime_projection SET
                core_boot_id = :coreBootId,
                last_sequence = :lastSequence,
                runtime_revision = :runtimeRevision,
                gap_detected = :gapDetected,
                last_event_at = :lastEventAt,
                captured_at = :capturedAt,
                users = CAST(:users AS jsonb),
                boards = CAST(:boards AS jsonb),
                sessions = CAST(:sessions AS jsonb),
                session_details_complete = :sessionDetailsComplete,
                projection_version = :version
            WHERE node_id = :nodeId
            """.trimIndent(),
            mapOf(
                "nodeId" to projection.nodeId,
                "coreBootId" to projection.coreBootId,
                "lastSequence" to projection.lastSequence,
                "runtimeRevision" to projection.runtimeRevision,
                "gapDetected" to projection.gapDetected,
                "lastEventAt" to projection.lastEventAt?.toSqlTimestamp(),
                "capturedAt" to projection.capturedAt?.toSqlTimestamp(),
                "users" to json.writeValueAsString(projection.users),
                "boards" to json.writeValueAsString(projection.boards),
                "sessions" to json.writeValueAsString(projection.sessions),
                "sessionDetailsComplete" to projection.sessionDetailsComplete,
                "version" to projection.version,
            ),
        )
        check(updated == 1) { "runtime projection update failed for node ${projection.nodeId}" }
    }

    override fun projection(nodeId: String): RuntimeProjection? = jdbc.query(
        "$PROJECTION_SELECT WHERE node_id = :nodeId",
        mapOf("nodeId" to nodeId),
    ) { rs, _ -> mapProjection(rs) }.firstOrNull()

    override fun events(
        nodeId: String,
        coreBootId: String?,
        afterSequence: Long?,
        limit: Int,
    ): List<RuntimeEventView> {
        val cursorQuery = coreBootId != null
        val sql = buildString {
            append(
                """
                SELECT event_id, core_boot_id, sequence_number, runtime_revision,
                       event_type, payload::text, occurred_at, received_at
                FROM runtime_events
                WHERE node_id = :nodeId
                """.trimIndent(),
            )
            if (cursorQuery) append(" AND core_boot_id = :coreBootId")
            if (afterSequence != null) append(" AND sequence_number > :afterSequence")
            append(if (cursorQuery) " ORDER BY sequence_number ASC, occurred_at ASC" else " ORDER BY occurred_at DESC")
            append(" LIMIT :limit")
        }
        return jdbc.query(
            sql,
            mapOf(
                "nodeId" to nodeId,
                "coreBootId" to coreBootId,
                "afterSequence" to afterSequence,
                "limit" to limit,
            ),
        ) { rs, _ -> mapEvent(rs) }
    }

    override fun material(nodeId: String): RuntimeReplayMaterial? {
        val snapshot = jdbc.query(
            """
            SELECT snapshot::text FROM runtime_event_batches
            WHERE node_id = :nodeId AND snapshot IS NOT NULL
            ORDER BY received_at DESC LIMIT 1
            """.trimIndent(),
            mapOf("nodeId" to nodeId),
        ) { rs, _ -> json.readValue(rs.getString(1), RuntimeSnapshot::class.java) }.firstOrNull() ?: return null
        val events = jdbc.query(
            """
            SELECT event_id, core_boot_id, sequence_number, runtime_revision,
                   event_type, payload::text, occurred_at
            FROM runtime_events
            WHERE node_id = :nodeId AND core_boot_id = :coreBootId
              AND (sequence_number > :sequence OR (sequence_number = 0 AND occurred_at > :capturedAt))
            ORDER BY sequence_number, occurred_at, event_id
            """.trimIndent(),
            mapOf(
                "nodeId" to nodeId,
                "coreBootId" to snapshot.coreBootId,
                "sequence" to snapshot.latestSequence,
                "capturedAt" to snapshot.capturedAt.toSqlTimestamp(),
            ),
        ) { rs, _ -> mapDomainEvent(rs) }
        return RuntimeReplayMaterial(snapshot, events)
    }

    private fun mapProjection(rs: ResultSet) = RuntimeProjection(
        nodeId = rs.getString("node_id"),
        coreBootId = rs.getString("core_boot_id"),
        lastSequence = rs.getLong("last_sequence"),
        runtimeRevision = rs.getLong("runtime_revision"),
        gapDetected = rs.getBoolean("gap_detected"),
        lastEventAt = rs.getTimestamp("last_event_at")?.toInstant(),
        capturedAt = rs.getTimestamp("captured_at")?.toInstant(),
        users = json.readValue(rs.getString("users"), USERS_TYPE),
        boards = json.readValue(rs.getString("boards"), BOARDS_TYPE),
        sessions = json.readValue(rs.getString("sessions"), SESSIONS_TYPE),
        sessionDetailsComplete = rs.getBoolean("session_details_complete"),
        version = rs.getLong("projection_version"),
    )

    private fun mapEvent(rs: ResultSet) = RuntimeEventView(
        eventId = rs.getString("event_id"),
        coreBootId = rs.getString("core_boot_id"),
        sequence = rs.getLong("sequence_number"),
        runtimeRevision = rs.getLong("runtime_revision"),
        type = rs.getString("event_type"),
        payload = json.readValue(rs.getString("payload"), PAYLOAD_TYPE),
        occurredAt = rs.getTimestamp("occurred_at").toInstant(),
        receivedAt = rs.getTimestamp("received_at").toInstant(),
    )

    private fun mapDomainEvent(rs: ResultSet): RuntimeEvent {
        val payload = json.readValue(rs.getString("payload"), PAYLOAD_TYPE)
        fun text(name: String) = payload[name]?.toString().orEmpty()
        fun long(name: String) = (payload[name] as? Number)?.toLong() ?: text(name).toLongOrNull() ?: 0L
        val value = when (rs.getString("event_type")) {
            "resource.changed" -> RuntimeEventPayload.ResourceChanged(
                RuntimeResourceKind.valueOf(text("kind").uppercase()),
                RuntimeResourceOperation.valueOf(text("operation").uppercase()),
                text("tag"),
            )
            "board.state.changed" -> RuntimeEventPayload.BoardStateChanged(
                text("boardTag"), text("previousState"), text("state"), text("error"),
            )
            "client.session.opened" -> RuntimeEventPayload.ClientSessionOpened(
                text("userTag"), text("boardTag"), text("bundleId"),
            )
            "client.session.closed" -> RuntimeEventPayload.ClientSessionClosed(
                text("userTag"), text("boardTag"), text("bundleId"), long("rxBytes"), long("txBytes"), text("reason"),
            )
            "stream.reset" -> RuntimeEventPayload.StreamReset(
                text("reason"), long("oldestAvailableSequence"), long("latestSequence"),
            )
            else -> error("unsupported persisted runtime event type ${rs.getString("event_type")}")
        }
        return RuntimeEvent(
            rs.getString("event_id"), rs.getString("core_boot_id"), rs.getLong("sequence_number"),
            rs.getTimestamp("occurred_at").toInstant(), rs.getLong("runtime_revision"), value,
        )
    }

    private fun eventPayload(payload: RuntimeEventPayload): Map<String, Any?> = when (payload) {
        is RuntimeEventPayload.ResourceChanged -> mapOf(
            "kind" to payload.kind.name.lowercase(),
            "operation" to payload.operation.name.lowercase(),
            "tag" to payload.tag,
        )
        is RuntimeEventPayload.BoardStateChanged -> mapOf(
            "boardTag" to payload.boardTag,
            "previousState" to payload.previousState,
            "state" to payload.state,
            "error" to payload.error,
        )
        is RuntimeEventPayload.ClientSessionOpened -> mapOf(
            "userTag" to payload.userTag,
            "boardTag" to payload.boardTag,
            "bundleId" to payload.bundleId,
        )
        is RuntimeEventPayload.ClientSessionClosed -> mapOf(
            "userTag" to payload.userTag,
            "boardTag" to payload.boardTag,
            "bundleId" to payload.bundleId,
            "rxBytes" to payload.rxBytes,
            "txBytes" to payload.txBytes,
            "reason" to payload.reason,
        )
        is RuntimeEventPayload.StreamReset -> mapOf(
            "reason" to payload.reason,
            "oldestAvailableSequence" to payload.oldestAvailableSequence,
            "latestSequence" to payload.latestSequence,
        )
    }

    private companion object {
        const val PROJECTION_SELECT = """
            SELECT node_id, core_boot_id, last_sequence, runtime_revision, gap_detected,
                   last_event_at, captured_at, users::text, boards::text, sessions::text,
                   session_details_complete, projection_version
            FROM node_runtime_projection
        """
        val USERS_TYPE = object : TypeReference<Map<String, RuntimeUserState>>() {}
        val BOARDS_TYPE = object : TypeReference<Map<String, RuntimeBoardState>>() {}
        val SESSIONS_TYPE = object : TypeReference<Map<String, RuntimeSessionState>>() {}
        val PAYLOAD_TYPE = object : TypeReference<Map<String, Any?>>() {}
    }
}
