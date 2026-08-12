package io.boardproxy.control.shared.events.postgres

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.shared.events.OutboxEvent
import io.boardproxy.control.shared.events.OutboxDeliveryRepository
import io.boardproxy.control.shared.events.OutboxRepository
import io.boardproxy.control.shared.events.OutboxDeadLetter
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import org.springframework.transaction.support.TransactionTemplate
import java.sql.ResultSet

@Repository
class PostgresOutboxRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val json: ObjectMapper,
    private val transactions: TransactionTemplate,
) : OutboxRepository, OutboxDeliveryRepository {
    override fun append(event: OutboxEvent) {
        jdbc.update(
            """
            INSERT INTO outbox_events (
                event_id, aggregate_type, aggregate_id, event_type,
                payload, occurred_at
            ) VALUES (
                :id, :aggregateType, :aggregateId, :type,
                CAST(:payload AS jsonb), :occurredAt
            )
            """.trimIndent(),
            mapOf(
                "id" to event.id, "aggregateType" to event.aggregateType,
                "aggregateId" to event.aggregateId, "type" to event.type,
                "payload" to json.writeValueAsString(event.payload),
                "occurredAt" to event.occurredAt.toSqlTimestamp(),
            ),
        )
    }

    override fun publishPending(limit: Int): Int {
        val eventIds = jdbc.queryForList(
            """
            SELECT event_id FROM outbox_events
            WHERE published_at IS NULL AND dead_lettered_at IS NULL
              AND (next_attempt_at IS NULL OR next_attempt_at <= now())
            ORDER BY COALESCE(next_attempt_at, occurred_at)
            LIMIT :limit
            """.trimIndent(),
            mapOf("limit" to limit),
            String::class.java,
        )
        var published = 0
        eventIds.forEach { eventId ->
            runCatching { transactions.execute { publishLocked(eventId) } }
                .onSuccess { result -> if (result == true) published++ }
                .onFailure { error -> markFailure(eventId, error) }
        }
        return published
    }

    private fun publishLocked(eventId: String): Boolean {
        val locked = jdbc.queryForList(
            """
            SELECT event_id FROM outbox_events
            WHERE event_id = :eventId AND published_at IS NULL AND dead_lettered_at IS NULL
              AND (next_attempt_at IS NULL OR next_attempt_at <= now())
            FOR UPDATE SKIP LOCKED
            """.trimIndent(),
            mapOf("eventId" to eventId),
            String::class.java,
        ).isNotEmpty()
        if (!locked) return false
        jdbc.jdbcTemplate.queryForObject(
            "SELECT pg_notify(?, ?)",
            String::class.java,
            CHANNEL,
            json.writeValueAsString(NotificationEnvelope.outbox(eventId)),
        )
        jdbc.update(
            """
            UPDATE outbox_events
            SET published_at = now(), attempts = attempts + 1, last_error = NULL, next_attempt_at = NULL
            WHERE event_id = :eventId
            """.trimIndent(),
            mapOf("eventId" to eventId),
        )
        return true
    }

    private fun markFailure(eventId: String, error: Throwable) {
        jdbc.update(
            """
            UPDATE outbox_events SET
                attempts = attempts + 1,
                last_error = :error,
                next_attempt_at = now() + make_interval(secs => LEAST(3600, CAST(power(2, attempts) AS integer))),
                dead_lettered_at = CASE WHEN attempts + 1 >= :maxAttempts THEN now() ELSE NULL END
            WHERE event_id = :eventId AND published_at IS NULL
            """.trimIndent(),
            mapOf(
                "eventId" to eventId,
                "error" to (error.message ?: error.javaClass.simpleName).take(2_000),
                "maxAttempts" to MAX_ATTEMPTS,
            ),
        )
    }

    override fun find(eventId: String): OutboxEvent? = jdbc.query(
        """
        SELECT event_id, aggregate_type, aggregate_id, event_type, payload::text, occurred_at
        FROM outbox_events WHERE event_id = :eventId
        """.trimIndent(),
        mapOf("eventId" to eventId),
    ) { rs, _ -> map(rs) }.firstOrNull()

    override fun deadLetters(limit: Int): List<OutboxDeadLetter> = jdbc.query(
        """
        SELECT event_id, aggregate_type, aggregate_id, event_type, payload::text, occurred_at,
               attempts, last_error, dead_lettered_at
        FROM outbox_events
        WHERE dead_lettered_at IS NOT NULL
        ORDER BY dead_lettered_at DESC LIMIT :limit
        """.trimIndent(),
        mapOf("limit" to limit),
    ) { rs, _ ->
        OutboxDeadLetter(map(rs), rs.getInt("attempts"), rs.getString("last_error"), rs.getTimestamp("dead_lettered_at").toInstant())
    }

    override fun retry(eventId: String): Boolean = jdbc.update(
        """
        UPDATE outbox_events SET attempts = 0, last_error = NULL,
            dead_lettered_at = NULL, next_attempt_at = now()
        WHERE event_id = :eventId AND dead_lettered_at IS NOT NULL
        """.trimIndent(),
        mapOf("eventId" to eventId),
    ) == 1

    @Suppress("UNCHECKED_CAST")
    private fun map(rs: ResultSet) = OutboxEvent(
        id = rs.getString("event_id"), aggregateType = rs.getString("aggregate_type"),
        aggregateId = rs.getString("aggregate_id"), type = rs.getString("event_type"),
        payload = json.readValue(rs.getString("payload"), Map::class.java) as Map<String, Any>,
        occurredAt = rs.getTimestamp("occurred_at").toInstant(),
    )

    data class NotificationEnvelope(
        val kind: String,
        val eventId: String? = null,
        val event: io.boardproxy.control.shared.events.ControlEvent? = null,
    ) {
        companion object {
            fun outbox(eventId: String) = NotificationEnvelope("outbox", eventId = eventId)
            fun realtime(event: io.boardproxy.control.shared.events.ControlEvent) =
                NotificationEnvelope("realtime", event = event)
        }
    }

    companion object {
        const val CHANNEL = "boardproxy_control_events"
        const val MAX_ATTEMPTS = 10
    }
}
