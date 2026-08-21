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
    override fun interfaceTotals(nodeId: String?, from: Instant, to: Instant): List<TrafficTotal> = jdbc.query(
        """
        SELECT delta.interface_name AS subject,
               COALESCE(SUM(delta.rx_bytes), 0) AS rx_bytes,
               COALESCE(SUM(delta.tx_bytes), 0) AS tx_bytes
        FROM interface_traffic_deltas delta
        WHERE ${scope(nodeId)} AND delta.observed_at > :from AND delta.observed_at <= :to
        GROUP BY delta.interface_name ORDER BY delta.interface_name
        """.trimIndent(),
        parameters(nodeId, from, to),
    ) { rs, _ -> rs.toTotal() }

    override fun userTotals(nodeId: String?, from: Instant, to: Instant): List<TrafficTotal> = jdbc.query(
        """
        SELECT delta.user_id AS subject,
               COALESCE(SUM(delta.rx_bytes), 0) AS rx_bytes,
               COALESCE(SUM(delta.tx_bytes), 0) AS tx_bytes
        FROM user_traffic_deltas delta
        WHERE ${scope(nodeId)} AND delta.observed_at > :from AND delta.observed_at <= :to
        GROUP BY delta.user_id ORDER BY delta.user_id
        """.trimIndent(),
        parameters(nodeId, from, to),
    ) { rs, _ -> rs.toTotal() }

    override fun nodeTotals(kind: TrafficKind, from: Instant, to: Instant): List<TrafficTotal> = jdbc.query(
        """
        SELECT delta.agent_id AS subject,
               COALESCE(SUM(delta.rx_bytes), 0) AS rx_bytes,
               COALESCE(SUM(delta.tx_bytes), 0) AS tx_bytes
        FROM ${deltas(kind)} delta
        WHERE delta.observed_at > :from AND delta.observed_at <= :to
        GROUP BY delta.agent_id ORDER BY delta.agent_id
        """.trimIndent(),
        parameters(null, from, to),
    ) { rs, _ -> rs.toTotal() }

    override fun series(
        nodeId: String?,
        kind: TrafficKind,
        from: Instant,
        to: Instant,
        bucketSeconds: Long,
    ): List<TrafficPoint> {
        val subject = if (kind == TrafficKind.INTERFACE) "delta.interface_name" else "delta.user_id"
        val sql = """
            SELECT date_bin(make_interval(secs => :bucketSeconds), delta.observed_at, TIMESTAMPTZ '1970-01-01') AS bucket,
                   $subject AS subject,
                   SUM(delta.rx_bytes) AS rx_bytes,
                   SUM(delta.tx_bytes) AS tx_bytes
            FROM ${deltas(kind)} delta
                WHERE ${scope(nodeId)} AND delta.observed_at > :from AND delta.observed_at <= :to
            GROUP BY bucket, subject ORDER BY bucket, subject
        """.trimIndent()
        return jdbc.query(sql, parameters(nodeId, from, to) + ("bucketSeconds" to bucketSeconds)) { rs, _ ->
            TrafficPoint(
                rs.getTimestamp("bucket").toInstant(), rs.getString("subject"),
                rs.getLong("rx_bytes"), rs.getLong("tx_bytes"),
            )
        }
    }

    private fun java.sql.ResultSet.toTotal() =
        TrafficTotal(getString("subject"), getLong("rx_bytes"), getLong("tx_bytes"))

    private fun deltas(kind: TrafficKind) =
        if (kind == TrafficKind.INTERFACE) "interface_traffic_deltas" else "user_traffic_deltas"

    /** Масштаб флота — это отсутствие фильтра, а не перебор нод по одной. */
    private fun scope(nodeId: String?) = if (nodeId == null) "TRUE" else "delta.agent_id = :nodeId"

    private fun parameters(nodeId: String?, from: Instant, to: Instant) =
        mapOf("nodeId" to nodeId, "from" to from.toSqlTimestamp(), "to" to to.toSqlTimestamp())
}
