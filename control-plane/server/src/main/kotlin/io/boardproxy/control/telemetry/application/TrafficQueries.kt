package io.boardproxy.control.telemetry.application

import java.time.Instant

data class TrafficTotal(val subject: String, val rxBytes: Long, val txBytes: Long)

data class TrafficPoint(
    val bucket: Instant,
    val subject: String,
    val rxBytes: Long,
    val txBytes: Long,
)

interface TrafficQueries {
    fun interfaceTotals(nodeId: String, from: Instant, to: Instant): List<TrafficTotal>
    fun userTotals(nodeId: String, from: Instant, to: Instant): List<TrafficTotal>
    fun series(nodeId: String, kind: TrafficKind, from: Instant, to: Instant, bucketSeconds: Long): List<TrafficPoint>
}

enum class TrafficKind { INTERFACE, USER }
