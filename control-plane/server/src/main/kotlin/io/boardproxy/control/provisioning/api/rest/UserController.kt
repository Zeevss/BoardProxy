package io.boardproxy.control.provisioning.api.rest

import io.boardproxy.control.provisioning.application.GrantInput
import io.boardproxy.control.provisioning.application.Page
import io.boardproxy.control.provisioning.application.UserInput
import io.boardproxy.control.provisioning.application.UserService
import io.boardproxy.control.provisioning.domain.model.Grant
import io.boardproxy.control.provisioning.domain.model.User
import io.boardproxy.control.shared.contracts.KeylinkQueries
import io.boardproxy.control.shared.contracts.NodeKeylink
import org.springframework.http.HttpStatus
import org.springframework.http.ResponseEntity
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.DeleteMapping
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.PutMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestHeader
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.RestController
import java.security.Principal
import java.time.Instant

/**
 * Пользователь флотовый: одна запись на человека. На каких нодах он работает,
 * описывают гранты — отдельный подресурс, а не поле пользователя.
 */
@RestController
@RequestMapping("/api/v1/users")
class UserController(
    private val service: UserService,
    private val keylinks: KeylinkQueries,
) {
    @GetMapping
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun list(
        @RequestParam(required = false) query: String?,
        @RequestParam(required = false) nodeId: String?,
        @RequestParam(defaultValue = "0") offset: Int,
        @RequestParam(defaultValue = "50") limit: Int,
    ): Page<UserResponse> = service.list(query, nodeId, offset, limit.coerceIn(1, MAXIMUM_PAGE))
        .let { Page(it.items.map(User::toResponse), it.offset, it.limit, it.total) }

    @GetMapping("/{id}")
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun get(@PathVariable id: String): ResponseEntity<UserResponse> = service.get(id).ok()

    @PostMapping
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun create(@RequestBody request: UserRequest, principal: Principal): ResponseEntity<UserResponse> {
        val user = service.create(request.toInput(), principal.name)
        return ResponseEntity.status(HttpStatus.CREATED).eTag(user.version.toString()).body(user.toResponse())
    }

    @PutMapping("/{id}")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun update(
        @PathVariable id: String,
        @RequestHeader("If-Match") ifMatch: String,
        @RequestBody request: UserRequest,
        principal: Principal,
    ): ResponseEntity<UserResponse> =
        service.update(id, ifMatch.version("user"), request.toInput(), principal.name).ok()

    @DeleteMapping("/{id}")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun delete(
        @PathVariable id: String,
        @RequestHeader("If-Match") ifMatch: String,
        principal: Principal,
    ): ResponseEntity<Void> {
        service.delete(id, ifMatch.version("user"), principal.name)
        return ResponseEntity.noContent().build()
    }

    @GetMapping("/{id}/grants")
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun grants(@PathVariable id: String): List<GrantResponse> = service.grantsOf(id).map(Grant::toResponse)

    /** Размещения заменяются целиком: частичные правки порождали рассинхрон. */
    @PutMapping("/{id}/grants")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun replaceGrants(
        @PathVariable id: String,
        @RequestBody request: List<GrantRequest>,
        principal: Principal,
    ): List<GrantResponse> = service
        .replaceGrants(id, request.map { GrantInput(it.nodeId, it.boardIds) }, principal.name)
        .map(Grant::toResponse)

    /** Обесценивает все ранее выданные ссылки этого пользователя на всех нодах. */
    @PostMapping("/{id}/key/rotate")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun rotateKey(
        @PathVariable id: String,
        @RequestHeader("If-Match") ifMatch: String,
        principal: Principal,
    ): ResponseEntity<UserResponse> = service.rotateKey(id, ifMatch.version("user"), principal.name).ok()

    @GetMapping("/{id}/keylinks")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun keylinks(@PathVariable id: String): List<NodeKeylink> =
        keylinks.forUser(id, service.get(id).name)

    private fun User.ok() = ResponseEntity.ok().eTag(version.toString()).body(toResponse())

    private companion object {
        const val MAXIMUM_PAGE = 200
    }
}

data class UserRequest(
    val id: String? = null,
    val name: String,
    /** Задан — хаб хранит только публичный ключ и keylink собрать не может. */
    val publicKey: String? = null,
    val state: String = "enabled",
    val maxSessions: Int = 0,
    val maxLanes: Int = 4,
)

/** Пустой набор бордов означает «все включённые борды ноды». */
data class GrantRequest(val nodeId: String, val boardIds: Set<String> = emptySet())

data class GrantResponse(val nodeId: String, val boardIds: List<String>)

data class UserResponse(
    val id: String,
    val name: String,
    /** Отпечаток вместо ключа: приватный ключ наружу не выходит. */
    val identityFingerprint: String,
    val hubIssuedKey: Boolean,
    val state: String,
    val maxSessions: Int,
    val maxLanes: Int,
    val version: Long,
    val updatedAt: Instant,
)

private fun UserRequest.toInput() = UserInput(
    id = id, name = name, publicKey = publicKey, state = state.resourceState(),
    maxSessions = maxSessions, maxLanes = maxLanes,
)

private fun Grant.toResponse() = GrantResponse(nodeId, boardIds.sorted())

private fun User.toResponse() = UserResponse(
    id = id, name = name, identityFingerprint = identityFingerprint(),
    hubIssuedKey = privateKey != null, state = state.name.lowercase(),
    maxSessions = maxSessions, maxLanes = maxLanes, version = version, updatedAt = updatedAt,
)
