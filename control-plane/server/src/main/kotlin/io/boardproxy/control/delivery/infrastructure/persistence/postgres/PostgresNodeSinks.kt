package io.boardproxy.control.delivery.infrastructure.persistence.postgres

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.delivery.application.InterfaceTrafficInput
import io.boardproxy.control.delivery.application.NodeRuntimeSink
import io.boardproxy.control.delivery.application.NodeTrafficSink
import io.boardproxy.control.delivery.application.RuntimeEventInput
import io.boardproxy.control.delivery.application.RuntimeSnapshotInput
import io.boardproxy.control.delivery.application.UserTrafficInput
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository

/**
 * Дельты привязаны к отчёту, а не к собственной таблице батчей: идемпотентность
 * обеспечивает agent_reports, поэтому отдельной схемы дедупликации у трафика
 * больше нет.
 */
@Repository
class PostgresNodeTrafficSink(private val jdbc: NamedParameterJdbcTemplate) : NodeTrafficSink {

    override fun record(
        nodeId: String,
        batchId: String,
        interfaces: List<InterfaceTrafficInput>,
        users: List<UserTrafficInput>,
    ) {
        if (interfaces.isNotEmpty()) {
            jdbc.batchUpdate(
                """
                INSERT INTO interface_traffic_deltas (
                    agent_id, batch_id, interface_name, rx_bytes, tx_bytes, rx_packets,
                    tx_packets, rx_errors, tx_errors, rx_dropped, tx_dropped, observed_at
                ) VALUES (
                    :agentId, :batchId, :name, :rxBytes, :txBytes, :rxPackets,
                    :txPackets, :rxErrors, :txErrors, :rxDropped, :txDropped, :observedAt
                )
                ON CONFLICT DO NOTHING
                """.trimIndent(),
                interfaces.map { delta ->
                    mapOf(
                        "agentId" to nodeId, "batchId" to batchId, "name" to delta.interfaceName,
                        "rxBytes" to delta.rxBytes, "txBytes" to delta.txBytes,
                        "rxPackets" to delta.rxPackets, "txPackets" to delta.txPackets,
                        "rxErrors" to delta.rxErrors, "txErrors" to delta.txErrors,
                        "rxDropped" to delta.rxDropped, "txDropped" to delta.txDropped,
                        "observedAt" to delta.observedAt.toSqlTimestamp(),
                    )
                }.toTypedArray(),
            )
        }
        if (users.isNotEmpty()) {
            jdbc.batchUpdate(
                """
                INSERT INTO user_traffic_deltas (agent_id, batch_id, user_id, rx_bytes, tx_bytes, observed_at)
                VALUES (:agentId, :batchId, :userId, :rxBytes, :txBytes, :observedAt)
                ON CONFLICT DO NOTHING
                """.trimIndent(),
                users.map { delta ->
                    mapOf(
                        "agentId" to nodeId, "batchId" to batchId, "userId" to delta.userId,
                        "rxBytes" to delta.rxBytes, "txBytes" to delta.txBytes,
                        "observedAt" to delta.observedAt.toSqlTimestamp(),
                    )
                }.toTypedArray(),
            )
        }
    }
}

/**
 * Снимок заменяется целиком. Проекции по событиям нет: нода знает своё состояние
 * лучше, чем хаб мог бы восстановить его из журнала, поэтому присылает готовым.
 * Вместе с проекцией исчезли детекция разрывов, реплей и перестроение.
 */
@Repository
class PostgresNodeRuntimeSink(
    private val jdbc: NamedParameterJdbcTemplate,
    private val json: ObjectMapper,
) : NodeRuntimeSink {

    override fun replaceSnapshot(nodeId: String, snapshot: RuntimeSnapshotInput) {
        jdbc.update(
            """
            INSERT INTO node_runtime (node_id, snapshot, observed_at)
            VALUES (:nodeId, CAST(:snapshot AS jsonb), :observedAt)
            ON CONFLICT (node_id) DO UPDATE SET
                snapshot = EXCLUDED.snapshot,
                observed_at = EXCLUDED.observed_at
            """.trimIndent(),
            mapOf(
                "nodeId" to nodeId,
                "snapshot" to json.writeValueAsString(snapshot),
                "observedAt" to snapshot.capturedAt.toSqlTimestamp(),
            ),
        )
    }

    /** Журнал активности: append-only, ничего не проецирует, разрыв безвреден. */
    override fun appendEvents(nodeId: String, events: List<RuntimeEventInput>) {
        jdbc.batchUpdate(
            """
            INSERT INTO runtime_events (node_id, event_type, payload, occurred_at)
            VALUES (:nodeId, :type, CAST(:payload AS jsonb), :occurredAt)
            """.trimIndent(),
            events.map { event ->
                mapOf(
                    "nodeId" to nodeId, "type" to event.type,
                    "payload" to event.payloadJson.ifBlank { "{}" },
                    "occurredAt" to event.occurredAt.toSqlTimestamp(),
                )
            }.toTypedArray(),
        )
    }
}
