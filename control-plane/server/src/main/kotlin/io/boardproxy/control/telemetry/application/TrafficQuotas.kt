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
) {
    fun put(
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
            if (quota.action == QuotaAction.DISABLE && usage.enforcedAt == null) enforce(quota, usage.periodStart)
        }
    }

    private fun usage(quota: TrafficQuota): TrafficQuotaUsage {
        val (start, end) = period(quota.period, clock.instant())
        val used = quotas.usedBytes(quota.nodeId, quota.userTag, start, minOf(end, clock.instant()))
        val state = quotas.state(quota.nodeId, quota.userTag, start)
        return TrafficQuotaUsage(quota, start, end, used, used >= quota.limitBytes, state.first, state.second)
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
        val utc = ZonedDateTime.ofInstant(now, ZoneOffset.UTC)
        val start = when (period) {
            QuotaPeriod.DAILY -> utc.toLocalDate().atStartOfDay(ZoneOffset.UTC)
            QuotaPeriod.MONTHLY -> utc.withDayOfMonth(1).toLocalDate().atStartOfDay(ZoneOffset.UTC)
        }
        val end = when (period) {
            QuotaPeriod.DAILY -> start.plusDays(1)
            QuotaPeriod.MONTHLY -> start.plusMonths(1)
        }
        return start.toInstant() to end.toInstant()
    }
}
