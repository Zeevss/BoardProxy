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
    override fun interfaceTotals(nodeId: String?, from: Instant, to: Instant): List<TrafficTotal> =
        totals(nodeId, TrafficKind.INTERFACE, from, to)

    override fun userTotals(nodeId: String?, from: Instant, to: Instant): List<TrafficTotal> =
        totals(nodeId, TrafficKind.USER, from, to)

    override fun nodeTotals(kind: TrafficKind, from: Instant, to: Instant): List<TrafficTotal> = jdbc.query(
        PostgresTrafficSources.cte(kind, null) + """
            SELECT node_id AS subject, SUM(rx_bytes) AS rx_bytes, SUM(tx_bytes) AS tx_bytes
            FROM combined_traffic GROUP BY node_id ORDER BY node_id
        """.trimIndent(),
        parameters(null, from, to, useRollups = true),
    ) { rs, _ -> rs.toTotal() }

    override fun series(
        nodeId: String?,
        kind: TrafficKind,
        from: Instant,
        to: Instant,
        bucketSeconds: Long,
    ): List<TrafficPoint> {
        val useRollups = bucketSeconds >= 3_600 && bucketSeconds % 3_600 == 0L
        val sql = PostgresTrafficSources.cte(kind, nodeId) + """
            SELECT date_bin(make_interval(secs => :bucketSeconds), observed_at, TIMESTAMPTZ '1970-01-01') AS bucket,
                   subject, SUM(rx_bytes) AS rx_bytes, SUM(tx_bytes) AS tx_bytes
            FROM combined_traffic
            GROUP BY bucket, subject ORDER BY bucket, subject
        """.trimIndent()
        return jdbc.query(
            sql,
            parameters(nodeId, from, to, useRollups) + ("bucketSeconds" to bucketSeconds),
        ) { rs, _ ->
            TrafficPoint(
                rs.getTimestamp("bucket").toInstant(), rs.getString("subject"),
                rs.getLong("rx_bytes"), rs.getLong("tx_bytes"),
            )
        }
    }

    private fun java.sql.ResultSet.toTotal() =
        TrafficTotal(getString("subject"), getLong("rx_bytes"), getLong("tx_bytes"))

    private fun totals(nodeId: String?, kind: TrafficKind, from: Instant, to: Instant): List<TrafficTotal> = jdbc.query(
        PostgresTrafficSources.cte(kind, nodeId) + """
            SELECT subject, SUM(rx_bytes) AS rx_bytes, SUM(tx_bytes) AS tx_bytes
            FROM combined_traffic GROUP BY subject ORDER BY subject
        """.trimIndent(),
        parameters(nodeId, from, to, useRollups = true),
    ) { rs, _ -> rs.toTotal() }

    private fun parameters(nodeId: String?, from: Instant, to: Instant, useRollups: Boolean) =
        mapOf(
            "nodeId" to nodeId,
            "from" to from.toSqlTimestamp(),
            "to" to to.toSqlTimestamp(),
            "useRollups" to useRollups,
        )
}
