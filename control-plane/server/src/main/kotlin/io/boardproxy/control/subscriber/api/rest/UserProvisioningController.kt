package io.boardproxy.control.subscriber.api.rest

import io.boardproxy.control.subscriber.application.FleetUserQueries
import io.boardproxy.control.subscriber.application.ProvisionedKey
import io.boardproxy.control.subscriber.application.ProvisionedUser
import io.boardproxy.control.subscriber.application.UserProvisioningCommands
import io.boardproxy.control.subscriber.application.UserProvisioningRequest
import io.boardproxy.control.subscriber.application.TrafficLimitRequest
import io.boardproxy.control.subscriber.application.UserTarget
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.telemetry.domain.QuotaAction
import io.boardproxy.control.telemetry.domain.QuotaPeriod
import io.boardproxy.control.subscriber.domain.FleetUser
import io.boardproxy.control.subscriber.domain.TrafficLimit
import io.boardproxy.control.subscriber.domain.UserBoard
import io.boardproxy.control.subscriber.domain.UserPlacement
import io.boardproxy.control.subscriber.domain.UserSubscription
import org.springframework.http.HttpStatus
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.ResponseStatus
import org.springframework.web.bind.annotation.RestController
import java.security.Principal
import java.time.Instant

@RestController
@RequestMapping("/api/v1/users")
class UserProvisioningController(
    private val commands: UserProvisioningCommands,
    private val queries: FleetUserQueries,
) {
    @PostMapping
    @ResponseStatus(HttpStatus.CREATED)
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun create(@RequestBody request: CreateUserRequest, principal: Principal): ProvisionedUserResponse =
        commands.create(request.toApplication(), principal.name).toResponse()

    /** Флотовый список: пользователь control-plane со всеми своими размещениями. */
    @GetMapping
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun list(@RequestParam(required = false) query: String?): List<FleetUserResponse> =
        queries.list(query).map(FleetUser::toResponse)
}

data class CreateUserRequest(
    val id: String,
    val name: String,
    val targets: List<CreateUserTarget>,
    val maxSessions: Int = 0,
    val maxLanes: Int = 1,
    /** Лимит трафика ставится вместе с пользователем; отсутствие поля означает «без лимита». */
    val traffic: CreateUserTraffic? = null,
)

data class CreateUserTraffic(
    val limitBytes: Long,
    val period: String = "monthly",
    val action: String = "reset",
)

data class CreateUserTarget(val nodeId: String, val boardIds: List<String>, val keyName: String? = null)

data class ProvisionedUserResponse(
    val id: String,
    val name: String,
    val deliveryType: String,
    val subscriptionId: String?,
    val subscriptionUrl: String?,
    val keys: List<ProvisionedKeyResponse>,
)

data class ProvisionedKeyResponse(val id: String, val name: String, val nodeId: String, val keylink: String)

private fun CreateUserRequest.toApplication() = UserProvisioningRequest(
    id, name, targets.map { UserTarget(it.nodeId, it.boardIds, it.keyName) }, maxSessions, maxLanes,
    traffic = traffic?.toApplication(),
)

private fun CreateUserTraffic.toApplication() = TrafficLimitRequest(
    limitBytes = limitBytes,
    period = runCatching { QuotaPeriod.valueOf(period.trim().uppercase()) }
        .getOrElse { throw InvalidRequest("traffic period must be daily, weekly, monthly or none") },
    action = runCatching { QuotaAction.valueOf(action.trim().uppercase()) }
        .getOrElse { throw InvalidRequest("traffic action must be alert, reset or disable") },
)

private fun ProvisionedUser.toResponse() = ProvisionedUserResponse(
    id = id, name = name, deliveryType = delivery.type,
    subscriptionId = delivery.subscriptionId, subscriptionUrl = delivery.subscriptionUrl,
    keys = delivery.keys.map(ProvisionedKey::toResponse),
)

private fun ProvisionedKey.toResponse() = ProvisionedKeyResponse(id, name, nodeId, keylink)

data class FleetUserResponse(
    val id: String,
    val name: String,
    val state: String,
    val placements: List<UserPlacementResponse>,
    val limits: UserLimitsResponse,
    val subscription: UserSubscriptionResponse?,
    val updatedAt: Instant,
)

data class UserPlacementResponse(
    val nodeId: String,
    val nodeName: String,
    val state: String,
    val boards: List<UserBoardResponse>,
    val version: Long,
)

data class UserBoardResponse(val id: String, val name: String)

data class UserLimitsResponse(val maxDevices: Int, val maxPages: Int, val traffic: TrafficLimitResponse?)

data class TrafficLimitResponse(
    val limitBytes: Long,
    val usedBytes: Long,
    val period: String,
    val action: String,
    val enabled: Boolean,
    val exceeded: Boolean,
    val periodStart: Instant,
    val periodEnd: Instant,
)

data class UserSubscriptionResponse(val id: String, val name: String, val state: String)

private fun FleetUser.toResponse() = FleetUserResponse(
    id = id, name = name, state = state.name.lowercase(),
    placements = placements.map(UserPlacement::toResponse),
    limits = UserLimitsResponse(limits.maxDevices, limits.maxPages, limits.traffic?.toResponse()),
    subscription = subscription?.toResponse(),
    updatedAt = updatedAt,
)

private fun UserPlacement.toResponse() = UserPlacementResponse(
    nodeId, nodeName, state.name.lowercase(), boards.map { UserBoardResponse(it.id, it.name) }, version,
)

private fun TrafficLimit.toResponse() = TrafficLimitResponse(
    limitBytes = limitBytes, usedBytes = usedBytes, period = period.name.lowercase(),
    action = action.name.lowercase(), enabled = enabled, exceeded = exceeded,
    periodStart = periodStart, periodEnd = periodEnd,
)

private fun UserSubscription.toResponse() = UserSubscriptionResponse(id, name, state)
