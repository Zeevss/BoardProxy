package io.boardproxy.control.telemetry.application

import java.time.Instant

data class InterfaceDelta(
    val name: String,
    val rxBytes: Long,
    val txBytes: Long,
    val rxPackets: Long,
    val txPackets: Long,
    val rxErrors: Long,
    val txErrors: Long,
    val rxDropped: Long,
    val txDropped: Long,
)

data class UserDelta(val userTag: String, val rxBytes: Long, val txBytes: Long)

data class TrafficBatch<T>(
    val nodeId: String,
    val batchId: String,
    val intervalStart: Instant,
    val intervalEnd: Instant,
    val deltas: List<T>,
    val rawPayload: ByteArray,
)

interface TrafficIngestion {
    fun storeInterface(batch: TrafficBatch<InterfaceDelta>)
    fun storeUsers(batch: TrafficBatch<UserDelta>)
}
