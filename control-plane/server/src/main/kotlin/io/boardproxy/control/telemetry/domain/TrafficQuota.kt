package io.boardproxy.control.telemetry.domain

import java.time.Instant

/** NONE — лимит на всё время жизни пользователя, календарного сброса нет. */
enum class QuotaPeriod { DAILY, WEEKLY, MONTHLY, NONE }

/** ALERT только уведомляет, RESET начинает отсчёт заново, DISABLE выключает пользователя. */
enum class QuotaAction { ALERT, RESET, DISABLE }

/**
 * Квота флотовая: лимит у пользователя один, а расход суммируется по всем нодам,
 * где он размещён. Прежде квота была per-node, и панель складывала лимиты, чтобы
 * показать одно число.
 */
data class TrafficQuota(
    val userId: String,
    val period: QuotaPeriod,
    val limitBytes: Long,
    val action: QuotaAction,
    val enabled: Boolean,
    val version: Long,
    val updatedAt: Instant,
    /** Момент последнего сброса счётчика; null — отсчёт с начала календарного периода. */
    val counterStart: Instant? = null,
) {
    init {
        require(userId.isNotBlank()) { "quota identity is required" }
        require(limitBytes > 0 && version > 0) { "quota limit and version must be positive" }
    }
}

/** Состояние текущего периода. [exceeded] — вход компилятора конфигурации. */
data class TrafficQuotaState(
    val userId: String,
    val periodStart: Instant,
    val usedBytes: Long,
    val exceeded: Boolean,
    val changedAt: Instant,
)

data class TrafficQuotaUsage(
    val quota: TrafficQuota,
    val periodStart: Instant,
    val periodEnd: Instant,
    val usedBytes: Long,
    val exceeded: Boolean,
)
