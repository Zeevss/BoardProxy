package io.boardproxy.control.telemetry.infrastructure.persistence.postgres

import io.boardproxy.control.telemetry.application.InterfaceDelta
import io.boardproxy.control.telemetry.application.TrafficBatch
import io.boardproxy.control.telemetry.application.TrafficIngestion
import io.boardproxy.control.telemetry.application.UserDelta
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import org.springframework.transaction.annotation.Transactional

@Repository
class PostgresTrafficIngestion(private val jdbc: NamedParameterJdbcTemplate) : TrafficIngestion {
    @Transactional
    override fun storeInterface(batch: TrafficBatch<InterfaceDelta>) {
        if (!insertBatch(batch, "interface")) return
        batch.deltas.forEach { delta ->
            jdbc.update(
                """
                INSERT INTO interface_traffic_deltas (
                    node_id, batch_id, interface_name, rx_bytes, tx_bytes,
                    rx_packets, tx_packets, rx_errors, tx_errors, rx_dropped, tx_dropped
                ) VALUES (
                    :nodeId, :batchId, :name, :rxBytes, :txBytes,
                    :rxPackets, :txPackets, :rxErrors, :txErrors, :rxDropped, :txDropped
                )
                """.trimIndent(),
                mapOf(
                    "nodeId" to batch.nodeId, "batchId" to batch.batchId, "name" to delta.name,
                    "rxBytes" to delta.rxBytes, "txBytes" to delta.txBytes,
                    "rxPackets" to delta.rxPackets, "txPackets" to delta.txPackets,
                    "rxErrors" to delta.rxErrors, "txErrors" to delta.txErrors,
                    "rxDropped" to delta.rxDropped, "txDropped" to delta.txDropped,
                ),
            )
        }
    }

    @Transactional
    override fun storeUsers(batch: TrafficBatch<UserDelta>) {
        if (!insertBatch(batch, "user")) return
        batch.deltas.forEach { delta ->
            jdbc.update(
                """
                INSERT INTO user_traffic_deltas (node_id, batch_id, user_tag, rx_bytes, tx_bytes)
                VALUES (:nodeId, :batchId, :userTag, :rxBytes, :txBytes)
                """.trimIndent(),
                mapOf(
                    "nodeId" to batch.nodeId, "batchId" to batch.batchId, "userTag" to delta.userTag,
                    "rxBytes" to delta.rxBytes, "txBytes" to delta.txBytes,
                ),
            )
        }
    }

    private fun insertBatch(batch: TrafficBatch<*>, kind: String): Boolean = jdbc.update(
        """
        INSERT INTO traffic_batches (
            node_id, batch_id, traffic_kind, interval_start, interval_end, payload
        ) VALUES (
            :nodeId, :batchId, :kind, :intervalStart, :intervalEnd, :payload
        ) ON CONFLICT (node_id, batch_id) DO NOTHING
        """.trimIndent(),
        mapOf(
            "nodeId" to batch.nodeId, "batchId" to batch.batchId, "kind" to kind,
            "intervalStart" to batch.intervalStart.toSqlTimestamp(),
            "intervalEnd" to batch.intervalEnd.toSqlTimestamp(),
            "payload" to batch.rawPayload,
        ),
    ) == 1
}
