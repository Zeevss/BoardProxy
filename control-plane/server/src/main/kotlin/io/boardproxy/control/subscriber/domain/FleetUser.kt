package io.boardproxy.control.subscriber.domain

import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.telemetry.domain.QuotaAction
import io.boardproxy.control.telemetry.domain.QuotaPeriod
import java.time.Instant

/**
 * Пользователь как сущность control-plane: одна запись на весь флот.
 * Размещение по нодам — вторичная проекция, поэтому лежит внутри списком.
 */
data class FleetUser(
    val id: String,
    val name: String,
    val state: ResourceState,
    val placements: List<UserPlacement>,
    val limits: UserLimits,
    val subscription: UserSubscription?,
    val updatedAt: Instant,
) {
    init {
        require(id.isNotBlank() && name.isNotBlank()) { "fleet user identity is required" }
        require(placements.isNotEmpty()) { "fleet user must exist on at least one node" }
    }

    /** Один и тот же пользователь может быть по-разному включён на разных нодах. */
    val enabledSomewhere: Boolean get() = placements.any { it.state == ResourceState.ENABLED }
}

data class UserPlacement(
    val nodeId: String,
    val nodeName: String,
    val state: ResourceState,
    val boards: List<UserBoard>,
    val version: Long,
)

data class UserBoard(val id: String, val name: String)

/**
 * Лимиты пользователя в терминах панели: устройства, страницы и трафик.
 * Лимит трафика необязателен — его задаёт отдельная квота.
 */
data class UserLimits(
    val maxDevices: Int,
    val maxPages: Int,
    val traffic: TrafficLimit?,
)

data class TrafficLimit(
    val limitBytes: Long,
    val usedBytes: Long,
    val period: QuotaPeriod,
    val action: QuotaAction,
    val enabled: Boolean,
    val periodStart: Instant,
    val periodEnd: Instant,
) {
    val exceeded: Boolean get() = usedBytes >= limitBytes
}

data class UserSubscription(val id: String, val name: String, val state: String)
