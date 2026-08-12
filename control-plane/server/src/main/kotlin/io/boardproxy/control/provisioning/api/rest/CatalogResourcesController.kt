package io.boardproxy.control.provisioning.api.rest

import com.fasterxml.jackson.annotation.JsonProperty
import io.boardproxy.control.provisioning.application.BoardInput
import io.boardproxy.control.provisioning.application.CatalogResourceCommands
import io.boardproxy.control.provisioning.application.NodeInput
import io.boardproxy.control.provisioning.application.UserInput
import io.boardproxy.control.provisioning.domain.model.AssignedUser
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.shared.errors.InvalidRequest
import org.springframework.http.ResponseEntity
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.DeleteMapping
import org.springframework.web.bind.annotation.PatchMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.PutMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestHeader
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController
import java.security.Principal

@RestController
@RequestMapping("/api/v1/nodes/{nodeId}")
@PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
class CatalogResourcesController(private val commands: CatalogResourceCommands) {
    @PatchMapping
    fun updateNode(
        @PathVariable nodeId: String,
        @RequestHeader("If-Match") ifMatch: String,
        @RequestBody request: NodePatchRequest,
        principal: Principal,
    ) = response(commands.updateNode(nodeId, version(ifMatch), request.toInput(), principal.name))

    @PutMapping("/boards/{boardId}")
    fun putBoard(
        @PathVariable nodeId: String,
        @PathVariable boardId: String,
        @RequestHeader("If-Match") ifMatch: String,
        @RequestBody request: BoardResourceRequest,
        principal: Principal,
    ) = response(commands.putBoard(nodeId, boardId, version(ifMatch), request.toInput(), principal.name))

    @DeleteMapping("/boards/{boardId}")
    fun removeBoard(
        @PathVariable nodeId: String,
        @PathVariable boardId: String,
        @RequestHeader("If-Match") ifMatch: String,
        principal: Principal,
    ) = response(commands.removeBoard(nodeId, boardId, version(ifMatch), principal.name))

    @PutMapping("/users/{userId}")
    fun putUser(
        @PathVariable nodeId: String,
        @PathVariable userId: String,
        @RequestHeader("If-Match") ifMatch: String,
        @RequestBody request: UserResourceRequest,
        principal: Principal,
    ) = response(commands.putUser(nodeId, userId, version(ifMatch), request.toInput(), principal.name))

    @DeleteMapping("/users/{userId}")
    fun removeUser(
        @PathVariable nodeId: String,
        @PathVariable userId: String,
        @RequestHeader("If-Match") ifMatch: String,
        principal: Principal,
    ) = response(commands.removeUser(nodeId, userId, version(ifMatch), principal.name))

    @PutMapping("/assignment")
    fun replaceAssignment(
        @PathVariable nodeId: String,
        @RequestHeader("If-Match") ifMatch: String,
        @RequestBody request: AssignmentWriteRequest,
        principal: Principal,
    ) = response(
        commands.replaceAssignment(
            nodeId, version(ifMatch), request.boardIds,
            request.users.map { AssignedUser(it.userId, it.boardIds) }, principal.name,
        ),
    )

    private fun response(result: io.boardproxy.control.provisioning.application.CatalogMutationResult) =
        ResponseEntity.ok().eTag(result.catalog.version.toString()).body(result.toResponse())

    private fun version(ifMatch: String): Long = ifMatch.removeSurrounding("\"").toLongOrNull()
        ?: throw InvalidRequest("If-Match must contain the numeric catalog version")
}

data class NodePatchRequest(val name: String? = null, val state: String? = null) {
    fun toInput(): NodeInput {
        if (name == null && state == null) throw InvalidRequest("node patch must contain name or state")
        return NodeInput(name, state?.state())
    }
}

data class BoardResourceRequest(
    val name: String,
    val hash: String,
    val hubSlide: String? = null,
    val apiBase: String? = null,
    val guestName: String? = null,
    val state: String = "enabled",
    val maxLanes: Int = 1,
) {
    fun toInput() = BoardInput(name, hash, hubSlide, apiBase, guestName, state.state(), maxLanes)
}

data class UserResourceRequest(
    val name: String,
    @JsonProperty(access = JsonProperty.Access.WRITE_ONLY)
    val privateKey: String? = null,
    val publicKey: String? = null,
    val state: String = "enabled",
    val maxSessions: Int = 0,
    val maxLanes: Int = 1,
) {
    fun toInput() = UserInput(name, privateKey, publicKey, state.state(), maxSessions, maxLanes)
}

private fun String.state() = runCatching { ResourceState.valueOf(trim().uppercase()) }
    .getOrElse { throw InvalidRequest("state must be enabled, disabled or revoked") }
