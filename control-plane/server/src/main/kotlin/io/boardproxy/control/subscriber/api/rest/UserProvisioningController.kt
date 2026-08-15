package io.boardproxy.control.subscriber.api.rest

import io.boardproxy.control.subscriber.application.ProvisionedKey
import io.boardproxy.control.subscriber.application.ProvisionedUser
import io.boardproxy.control.subscriber.application.UserProvisioningCommands
import io.boardproxy.control.subscriber.application.UserProvisioningRequest
import io.boardproxy.control.subscriber.application.UserTarget
import org.springframework.http.HttpStatus
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.ResponseStatus
import org.springframework.web.bind.annotation.RestController
import java.security.Principal

@RestController
@RequestMapping("/api/v1/users")
@PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
class UserProvisioningController(private val commands: UserProvisioningCommands) {
    @PostMapping
    @ResponseStatus(HttpStatus.CREATED)
    fun create(@RequestBody request: CreateUserRequest, principal: Principal): ProvisionedUserResponse =
        commands.create(request.toApplication(), principal.name).toResponse()
}

data class CreateUserRequest(
    val id: String,
    val name: String,
    val targets: List<CreateUserTarget>,
    val maxSessions: Int = 0,
    val maxLanes: Int = 1,
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
)

private fun ProvisionedUser.toResponse() = ProvisionedUserResponse(
    id = id, name = name, deliveryType = delivery.type,
    subscriptionId = delivery.subscriptionId, subscriptionUrl = delivery.subscriptionUrl,
    keys = delivery.keys.map(ProvisionedKey::toResponse),
)

private fun ProvisionedKey.toResponse() = ProvisionedKeyResponse(id, name, nodeId, keylink)
