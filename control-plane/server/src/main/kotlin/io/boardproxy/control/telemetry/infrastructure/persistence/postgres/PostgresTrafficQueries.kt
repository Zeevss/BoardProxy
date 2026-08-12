package io.boardproxy.control.telemetry.infrastructure.persistence.postgres

import io.boardproxy.control.telemetry.application.TrafficQueries
import io.boardproxy.control.telemetry.application.TrafficTotal
import io.boardproxy.control.telemetry.application.TrafficKind
import io.boardproxy.control.telemetry.application.TrafficPoint
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.time.Instant

@Repository
class PostgresTrafficQueries(private val jdbc: NamedParameterJdbcTemplate) : TrafficQueries {
    override fun interfaceTotals(nodeId: String, from: Instant, to: Instant): List<TrafficTotal> = jdbc.query(
        """
        SELECT delta.interface_name AS subject,
               COALESCE(SUM(delta.rx_bytes), 0) AS rx_bytes,
               COALESCE(SUM(delta.tx_bytes), 0) AS tx_bytes
        FROM interface_traffic_deltas delta
        JOIN traffic_batches batch USING (node_id, batch_id)
        WHERE delta.node_id = :nodeId AND batch.interval_end > :from AND batch.interval_end <= :to
        GROUP BY delta.interface_name ORDER BY delta.interface_name
        """.trimIndent(),
        parameters(nodeId, from, to),
    ) { rs, _ -> TrafficTotal(rs.getString("subject"), rs.getLong("rx_bytes"), rs.getLong("tx_bytes")) }

    override fun userTotals(nodeId: String, from: Instant, to: Instant): List<TrafficTotal> = jdbc.query(
        """
        SELECT delta.user_tag AS subject,
               COALESCE(SUM(delta.rx_bytes), 0) AS rx_bytes,
               COALESCE(SUM(delta.tx_bytes), 0) AS tx_bytes
        FROM user_traffic_deltas delta
        JOIN traffic_batches batch USING (node_id, batch_id)
        WHERE delta.node_id = :nodeId AND batch.interval_end > :from AND batch.interval_end <= :to
        GROUP BY delta.user_tag ORDER BY delta.user_tag
        """.trimIndent(),
        parameters(nodeId, from, to),
    ) { rs, _ -> TrafficTotal(rs.getString("subject"), rs.getLong("rx_bytes"), rs.getLong("tx_bytes")) }

    override fun series(
        nodeId: String,
        kind: TrafficKind,
        from: Instant,
        to: Instant,
        bucketSeconds: Long,
    ): List<TrafficPoint> {
        val subject = if (kind == TrafficKind.INTERFACE) "delta.interface_name" else "delta.user_tag"
        val deltas = if (kind == TrafficKind.INTERFACE) "interface_traffic_deltas" else "user_traffic_deltas"
        val sql = """
            SELECT date_bin(make_interval(secs => :bucketSeconds), batch.interval_end, TIMESTAMPTZ '1970-01-01') AS bucket,
                   $subject AS subject,
                   SUM(delta.rx_bytes) AS rx_bytes,
                   SUM(delta.tx_bytes) AS tx_bytes
            FROM $deltas delta
            JOIN traffic_batches batch USING (node_id, batch_id)
            WHERE delta.node_id = :nodeId AND batch.interval_end > :from AND batch.interval_end <= :to
            GROUP BY bucket, subject ORDER BY bucket, subject
        """.trimIndent()
        return jdbc.query(sql, parameters(nodeId, from, to) + ("bucketSeconds" to bucketSeconds)) { rs, _ ->
            TrafficPoint(
                rs.getTimestamp("bucket").toInstant(), rs.getString("subject"),
                rs.getLong("rx_bytes"), rs.getLong("tx_bytes"),
            )
        }
    }

    private fun parameters(nodeId: String, from: Instant, to: Instant) =
        mapOf("nodeId" to nodeId, "from" to from.toSqlTimestamp(), "to" to to.toSqlTimestamp())
}
