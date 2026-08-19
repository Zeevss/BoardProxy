package io.boardproxy.control.telemetry.infrastructure.persistence.postgres

import io.boardproxy.control.shared.persistence.toSqlTimestamp
import io.boardproxy.control.telemetry.application.TrafficMaintenance
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import org.springframework.transaction.annotation.Transactional
import java.time.Instant

@Repository
class PostgresTrafficMaintenance(private val jdbc: NamedParameterJdbcTemplate) : TrafficMaintenance {
    @Transactional
    override fun rebuildHourly(from: Instant, to: Instant): Int {
        val parameters = mapOf("from" to from.toSqlTimestamp(), "to" to to.toSqlTimestamp())
        val interfaceRows = rebuild(
            "interface", "interface_traffic_deltas", "delta.interface_name", parameters,
        )
        val userRows = rebuild("user", "user_traffic_deltas", "delta.user_id", parameters)
        return interfaceRows + userRows
    }

    override fun deleteRawBefore(cutoff: Instant): Int = jdbc.update(
        "DELETE FROM agent_reports WHERE received_at < :cutoff",
        mapOf("cutoff" to cutoff.toSqlTimestamp()),
    )

    override fun deleteRollupsBefore(cutoff: Instant): Int = jdbc.update(
        "DELETE FROM traffic_hourly_rollups WHERE bucket_start < :cutoff",
        mapOf("cutoff" to cutoff.toSqlTimestamp()),
    )

    private fun rebuild(
        kind: String,
        table: String,
        subject: String,
        parameters: Map<String, Any>,
    ): Int = jdbc.update(
        """
        INSERT INTO traffic_hourly_rollups (
            node_id, traffic_kind, subject, bucket_start, rx_bytes, tx_bytes, updated_at
        )
        SELECT delta.agent_id, '$kind', $subject, date_trunc('hour', delta.observed_at),
               SUM(delta.rx_bytes), SUM(delta.tx_bytes), now()
        FROM $table delta
        WHERE delta.observed_at >= :from AND delta.observed_at < :to
        GROUP BY delta.agent_id, $subject, date_trunc('hour', delta.observed_at)
        ON CONFLICT (node_id, traffic_kind, subject, bucket_start) DO UPDATE SET
            rx_bytes = EXCLUDED.rx_bytes,
            tx_bytes = EXCLUDED.tx_bytes,
            updated_at = EXCLUDED.updated_at
        """.trimIndent(),
        parameters,
    )
}
