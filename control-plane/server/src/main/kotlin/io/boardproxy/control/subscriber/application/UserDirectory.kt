package io.boardproxy.control.subscriber.application

import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.subscriber.domain.FleetUser
import io.boardproxy.control.subscriber.domain.TrafficLimit
import io.boardproxy.control.subscriber.domain.UserLimits
import io.boardproxy.control.subscriber.domain.UserPlacement
import io.boardproxy.control.subscriber.domain.UserSubscription
import io.boardproxy.control.telemetry.application.TrafficQuotaService
import io.boardproxy.control.telemetry.domain.TrafficQuotaUsage
import java.time.Instant

/** Плоская запись каталога: всё, что известно о пользователе без учёта трафика. */
data class FleetUserRecord(
    val id: String,
    val name: String,
    val state: ResourceState,
    val placements: List<UserPlacement>,
    val maxDevices: Int,
    val maxPages: Int,
    val subscription: UserSubscription?,
    val updatedAt: Instant,
)

fun interface FleetUserRepository {
    fun list(query: String?): List<FleetUserRecord>
}

fun interface FleetUserQueries {
    fun list(query: String?): List<FleetUser>
}

/**
 * Собирает флотовое представление пользователя: каталог даёт размещения и лимиты
 * устройств/страниц, telemetry — фактический расход трафика.
 */
class FleetUserService(
    private val users: FleetUserRepository,
    private val quotas: TrafficQuotaService,
) : FleetUserQueries {
    override fun list(query: String?): List<FleetUser> {
        val records = users.list(query?.trim()?.takeUnless(String::isEmpty))
        if (records.isEmpty()) return emptyList()
        val usage = records
            .flatMap { record -> record.placements.map(UserPlacement::nodeId) }
            .distinct()
            .flatMap { nodeId -> quotas.list(nodeId).map { (nodeId to it.quota.userTag) to it } }
            .toMap()
        return records.map { record ->
            FleetUser(
                id = record.id,
                name = record.name,
                state = record.state,
                placements = record.placements,
                limits = UserLimits(
                    maxDevices = record.maxDevices,
                    maxPages = record.maxPages,
                    traffic = traffic(record, usage),
                ),
                subscription = record.subscription,
                updatedAt = record.updatedAt,
            )
        }
    }

    /**
     * Квоты хранятся и применяются по нодам, поэтому фактический разрешённый объём
     * пользователя во флоте — это сумма по нодам, а не одно из значений.
     * Период и политику берём у первой квоты: панель пишет их одинаково на все ноды.
     */
    private fun traffic(record: FleetUserRecord, usage: Map<Pair<String, String>, TrafficQuotaUsage>): TrafficLimit? {
        val found = record.placements
            .mapNotNull { usage[it.nodeId to record.id] }
            .sortedBy { it.quota.nodeId }
        val first = found.firstOrNull() ?: return null
        return TrafficLimit(
            limitBytes = found.sumOf { it.quota.limitBytes },
            usedBytes = found.sumOf { it.usedBytes },
            period = first.quota.period,
            action = first.quota.action,
            enabled = found.any { it.quota.enabled },
            periodStart = first.periodStart,
            periodEnd = first.periodEnd,
        )
    }
}
