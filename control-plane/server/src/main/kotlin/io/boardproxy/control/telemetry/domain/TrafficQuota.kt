package io.boardproxy.control.telemetry.domain

import java.time.Instant

enum class QuotaPeriod { DAILY, MONTHLY }
enum class QuotaAction { ALERT, DISABLE }

data class TrafficQuota(
    val nodeId: String,
    val userTag: String,
    val period: QuotaPeriod,
    val limitBytes: Long,
    val action: QuotaAction,
    val enabled: Boolean,
    val version: Long,
    val updatedAt: Instant,
) {
    init {
        require(nodeId.isNotBlank() && userTag.isNotBlank()) { "quota identity is required" }
        require(limitBytes > 0 && version > 0) { "quota limit and version must be positive" }
    }
}

data class TrafficQuotaUsage(
    val quota: TrafficQuota,
    val periodStart: Instant,
    val periodEnd: Instant,
    val usedBytes: Long,
    val exceeded: Boolean,
    val exceededAt: Instant?,
    val enforcedAt: Instant?,
)
