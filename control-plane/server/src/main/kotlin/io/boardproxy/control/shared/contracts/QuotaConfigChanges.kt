package io.boardproxy.control.shared.contracts

import java.time.Instant

data class PendingQuotaConfigChange(val userId: String, val generation: Long)

/** Durable-мост между telemetry и пересборкой desired config. */
interface QuotaConfigChangeRepository {
    fun mark(userId: String, at: Instant)
    fun find(userId: String): PendingQuotaConfigChange?
    fun pending(limit: Int): List<PendingQuotaConfigChange>
    fun complete(userId: String, generation: Long): Boolean
}
