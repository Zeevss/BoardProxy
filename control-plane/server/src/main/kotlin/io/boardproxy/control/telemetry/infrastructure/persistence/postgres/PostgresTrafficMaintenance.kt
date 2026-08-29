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

    @Transactional
    override fun deleteRollupsBefore(cutoff: Instant): Int {
        val parameters = mapOf("cutoff" to cutoff.toSqlTimestamp())
        jdbc.update(
            """
            INSERT INTO user_traffic_lifetime_totals (
                node_id, user_id, rx_bytes, tx_bytes, archived_at
            )
            SELECT node_id, subject, SUM(rx_bytes), SUM(tx_bytes), now()
            FROM traffic_hourly_rollups
            WHERE traffic_kind = 'user' AND bucket_start < :cutoff
              AND NOT EXISTS (
                  SELECT 1 FROM user_traffic_deltas raw
                  WHERE raw.agent_id = traffic_hourly_rollups.node_id
                    AND raw.user_id = traffic_hourly_rollups.subject
                    AND date_trunc('hour', raw.observed_at) = traffic_hourly_rollups.bucket_start
              )
            GROUP BY node_id, subject
            ON CONFLICT (node_id, user_id) DO UPDATE SET
                rx_bytes = user_traffic_lifetime_totals.rx_bytes + EXCLUDED.rx_bytes,
                tx_bytes = user_traffic_lifetime_totals.tx_bytes + EXCLUDED.tx_bytes,
                archived_at = EXCLUDED.archived_at
            """.trimIndent(),
            parameters,
        )
        return jdbc.update(
            """
            DELETE FROM traffic_hourly_rollups rollup
            WHERE bucket_start < :cutoff
              AND (
                  traffic_kind <> 'user'
                  OR NOT EXISTS (
                      SELECT 1 FROM user_traffic_deltas raw
                      WHERE raw.agent_id = rollup.node_id
                        AND raw.user_id = rollup.subject
                        AND date_trunc('hour', raw.observed_at) = rollup.bucket_start
                  )
              )
            """.trimIndent(),
            parameters,
        )
    }

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
