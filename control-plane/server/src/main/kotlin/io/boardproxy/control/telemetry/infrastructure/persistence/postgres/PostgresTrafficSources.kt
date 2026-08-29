package io.boardproxy.control.telemetry.infrastructure.persistence.postgres

import io.boardproxy.control.telemetry.application.TrafficKind

/**
 * Объединяет почасовые rollup и ещё не свёрнутые дельты без двойного счёта.
 *
 * Полные часы можно безопасно читать из rollup. Граничные неполные часы всегда
 * читаются из raw, а полный час без rollup (например, сразу после приёма
 * отчёта) тоже остаётся видимым через raw.
 */
internal object PostgresTrafficSources {
    fun cte(kind: TrafficKind, nodeId: String?): String {
        val table = if (kind == TrafficKind.INTERFACE) "interface_traffic_deltas" else "user_traffic_deltas"
        val subject = if (kind == TrafficKind.INTERFACE) "delta.interface_name" else "delta.user_id"
        val databaseKind = if (kind == TrafficKind.INTERFACE) "interface" else "user"
        val rawScope = if (nodeId == null) "TRUE" else "delta.agent_id = :nodeId"
        val rollupScope = if (nodeId == null) "TRUE" else "rollup.node_id = :nodeId"

        return """
            WITH bounds AS (
                SELECT
                    CASE
                        WHEN CAST(:from AS timestamptz) = date_trunc('hour', CAST(:from AS timestamptz))
                            THEN date_trunc('hour', CAST(:from AS timestamptz))
                        ELSE date_trunc('hour', CAST(:from AS timestamptz)) + interval '1 hour'
                    END AS full_from,
                    date_trunc('hour', CAST(:to AS timestamptz)) AS full_to
            ),
            combined_traffic AS (
                SELECT delta.agent_id AS node_id, $subject AS subject,
                       delta.observed_at, delta.rx_bytes, delta.tx_bytes
                FROM $table delta CROSS JOIN bounds
                WHERE $rawScope
                  AND delta.observed_at >= :from AND delta.observed_at < :to
                  AND (
                      NOT CAST(:useRollups AS boolean)
                      OR delta.observed_at < bounds.full_from
                      OR delta.observed_at >= bounds.full_to
                      OR NOT EXISTS (
                          SELECT 1 FROM traffic_hourly_rollups existing
                          WHERE existing.node_id = delta.agent_id
                            AND existing.traffic_kind = '$databaseKind'
                            AND existing.subject = $subject
                            AND existing.bucket_start = date_trunc('hour', delta.observed_at)
                      )
                  )
                UNION ALL
                SELECT rollup.node_id, rollup.subject, rollup.bucket_start,
                       rollup.rx_bytes, rollup.tx_bytes
                FROM traffic_hourly_rollups rollup CROSS JOIN bounds
                WHERE CAST(:useRollups AS boolean)
                  AND $rollupScope
                  AND rollup.traffic_kind = '$databaseKind'
                  AND rollup.bucket_start >= bounds.full_from
                  AND rollup.bucket_start < bounds.full_to
            )
        """.trimIndent() + "\n"
    }
}
