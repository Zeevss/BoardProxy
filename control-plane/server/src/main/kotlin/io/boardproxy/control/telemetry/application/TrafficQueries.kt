package io.boardproxy.control.telemetry.application

import java.time.Instant

data class TrafficTotal(val subject: String, val rxBytes: Long, val txBytes: Long)

data class TrafficPoint(
    val bucket: Instant,
    val subject: String,
    val rxBytes: Long,
    val txBytes: Long,
)

/**
 * Везде, где стоит `nodeId: String?`, `null` означает весь флот.
 *
 * Экран трафика по умолчанию открывается в масштабе флота, и собирать этот
 * масштаб на клиенте означало бы по запросу на ноду — при том, что база
 * складывает те же строки одним `GROUP BY`.
 */
interface TrafficQueries {
    fun interfaceTotals(nodeId: String?, from: Instant, to: Instant): List<TrafficTotal>
    fun userTotals(nodeId: String?, from: Instant, to: Instant): List<TrafficTotal>

    /** Разбивка по нодам: subject — это `nodeId`. */
    fun nodeTotals(kind: TrafficKind, from: Instant, to: Instant): List<TrafficTotal>

    fun series(
        nodeId: String?,
        kind: TrafficKind,
        from: Instant,
        to: Instant,
        bucketSeconds: Long,
    ): List<TrafficPoint>
}

enum class TrafficKind { INTERFACE, USER }
