package io.boardproxy.control.telemetry.application

import io.boardproxy.control.provisioning.application.CatalogQueries
import io.boardproxy.control.provisioning.application.CatalogResourceCommands
import io.boardproxy.control.provisioning.application.UserInput
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.errors.ResourceNotFound
import io.boardproxy.control.telemetry.domain.QuotaAction
import io.boardproxy.control.telemetry.domain.QuotaPeriod
import io.boardproxy.control.telemetry.domain.TrafficQuota
import io.boardproxy.control.telemetry.domain.TrafficQuotaUsage
import java.time.Clock
import java.time.DayOfWeek
import java.time.Instant
import java.time.ZoneOffset
import java.time.ZonedDateTime

interface TrafficQuotaRepository {
    fun find(nodeId: String, userTag: String): TrafficQuota?
    fun list(nodeId: String): List<TrafficQuota>
    fun save(quota: TrafficQuota, expectedVersion: Long?): Boolean
    fun delete(nodeId: String, userTag: String, expectedVersion: Long): Boolean
    fun enabled(): List<TrafficQuota>
    fun usedBytes(nodeId: String, userTag: String, from: Instant, to: Instant): Long
    fun recordExceeded(nodeId: String, userTag: String, periodStart: Instant, at: Instant): Boolean
    fun recordEnforced(nodeId: String, userTag: String, periodStart: Instant, at: Instant)
    fun state(nodeId: String, userTag: String, periodStart: Instant): Pair<Instant?, Instant?>
    fun startNewCounter(nodeId: String, userTag: String, at: Instant)
}

/** Узкий порт для потребителей вне telemetry: провижининг ставит квоту вместе с пользователем. */
fun interface TrafficQuotaCommands {
    fun put(
        nodeId: String,
        userTag: String,
        period: QuotaPeriod,
        limitBytes: Long,
        action: QuotaAction,
        enabled: Boolean,
        expectedVersion: Long?,
    ): TrafficQuota
}

fun interface TrafficQuotaNotifier {
    fun exceeded(usage: TrafficQuotaUsage)
}

class TrafficQuotaService(
    private val quotas: TrafficQuotaRepository,
    private val catalogs: CatalogQueries,
    private val resources: CatalogResourceCommands,
    private val notifier: TrafficQuotaNotifier,
    private val clock: Clock,
) : TrafficQuotaCommands {
    override fun put(
        nodeId: String,
        userTag: String,
        period: QuotaPeriod,
        limitBytes: Long,
        action: QuotaAction,
        enabled: Boolean,
        expectedVersion: Long?,
    ): TrafficQuota {
        val current = quotas.find(nodeId, userTag)
        if (current == null && expectedVersion != null) throw ResourceConflict("traffic quota does not exist")
        if (current != null && current.version != expectedVersion) throw ResourceConflict("traffic quota version changed")
        val value = TrafficQuota(
            nodeId, userTag, period, limitBytes, action, enabled,
            version = (current?.version ?: 0) + 1, updatedAt = clock.instant(),
            // Правка лимита не должна дарить пользователю новый цикл; устаревший сброс
            // всё равно отсекается началом периода.
            counterStart = current?.counterStart,
        )
        if (!quotas.save(value, expectedVersion)) throw ResourceConflict("traffic quota version changed")
        return value
    }

    fun list(nodeId: String): List<TrafficQuotaUsage> = quotas.list(nodeId).map(::usage)

    fun delete(nodeId: String, userTag: String, expectedVersion: Long) {
        if (!quotas.delete(nodeId, userTag, expectedVersion)) throw ResourceNotFound("traffic quota not found or changed")
    }

    fun evaluate() {
        quotas.enabled().forEach { quota ->
            val usage = usage(quota)
            if (!usage.exceeded) return@forEach
            if (quotas.recordExceeded(quota.nodeId, quota.userTag, usage.periodStart, clock.instant())) {
                notifier.exceeded(usage.copy(exceededAt = clock.instant()))
            }
            when (quota.action) {
                QuotaAction.DISABLE -> if (usage.enforcedAt == null) enforce(quota, usage.periodStart)
                QuotaAction.RESET -> quotas.startNewCounter(quota.nodeId, quota.userTag, clock.instant())
                QuotaAction.ALERT -> Unit
            }
        }
    }

    private fun usage(quota: TrafficQuota): TrafficQuotaUsage {
        val (periodStart, end) = period(quota.period, clock.instant())
        // Сброшенный счётчик сдвигает начало отсчёта внутрь периода, но окно самого периода не меняет.
        val countFrom = maxOf(periodStart, quota.counterStart ?: periodStart)
        val used = quotas.usedBytes(quota.nodeId, quota.userTag, countFrom, minOf(end, clock.instant()))
        val state = quotas.state(quota.nodeId, quota.userTag, periodStart)
        return TrafficQuotaUsage(quota, periodStart, end, used, used >= quota.limitBytes, state.first, state.second)
    }

    private fun enforce(quota: TrafficQuota, periodStart: Instant) {
        val catalog = catalogs.get(quota.nodeId)
        val user = catalog.users.firstOrNull { it.id == quota.userTag } ?: return
        if (user.state != ResourceState.ENABLED) return
        resources.putUser(
            quota.nodeId, quota.userTag, catalog.version,
            UserInput(user.name, null, null, ResourceState.DISABLED, user.maxSessions, user.maxLanes),
            "system:traffic-quota",
        )
        quotas.recordEnforced(quota.nodeId, quota.userTag, periodStart, clock.instant())
    }

    private fun period(period: QuotaPeriod, now: Instant): Pair<Instant, Instant> {
        if (period == QuotaPeriod.NONE) return Instant.EPOCH to LIFETIME_END
        val utc = ZonedDateTime.ofInstant(now, ZoneOffset.UTC)
        val start = when (period) {
            QuotaPeriod.DAILY -> utc.toLocalDate().atStartOfDay(ZoneOffset.UTC)
            QuotaPeriod.WEEKLY -> utc.with(DayOfWeek.MONDAY).toLocalDate().atStartOfDay(ZoneOffset.UTC)
            QuotaPeriod.MONTHLY -> utc.withDayOfMonth(1).toLocalDate().atStartOfDay(ZoneOffset.UTC)
            QuotaPeriod.NONE -> error("lifetime period is handled above")
        }
        val end = when (period) {
            QuotaPeriod.DAILY -> start.plusDays(1)
            QuotaPeriod.WEEKLY -> start.plusWeeks(1)
            QuotaPeriod.MONTHLY -> start.plusMonths(1)
            QuotaPeriod.NONE -> error("lifetime period is handled above")
        }
        return start.toInstant() to end.toInstant()
    }

    private companion object {
        /** Верхняя граница «без сброса»: конкретная дата вместо Instant.MAX, чтобы ложиться в timestamptz. */
        val LIFETIME_END: Instant = Instant.parse("9999-01-01T00:00:00Z")
    }
}
