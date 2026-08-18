package io.boardproxy.control.subscription.api.rest

import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.subscription.application.IssuedSubscription
import io.boardproxy.control.subscription.application.SubscriptionCommands
import io.boardproxy.control.subscription.application.SubscriptionDraft
import io.boardproxy.control.subscription.application.SubscriptionKeyDraft
import io.boardproxy.control.subscription.application.SubscriptionLinkBuilder
import io.boardproxy.control.subscription.application.SubscriptionQueries
import io.boardproxy.control.subscription.application.SubscriptionReplacement
import io.boardproxy.control.subscription.application.SubscriptionSnapshot
import io.boardproxy.control.subscription.domain.Subscription
import io.boardproxy.control.subscription.domain.SubscriptionState
import org.springframework.http.HttpStatus
import org.springframework.http.ResponseEntity
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.PutMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestHeader
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController
import java.security.Principal
import java.time.Instant

@RestController
@RequestMapping("/api/v1/subscriptions")
class SubscriptionController(
    private val commands: SubscriptionCommands,
    private val queries: SubscriptionQueries,
    private val links: SubscriptionLinkBuilder,
) {
    @PostMapping
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun create(
        @RequestBody request: CreateSubscriptionRequest,
        principal: Principal,
    ): ResponseEntity<IssuedSubscriptionResponse> {
        val issued = commands.create(request.toDraft(), principal.name)
        return ResponseEntity.status(HttpStatus.CREATED)
            .eTag(issued.subscription.version.toString())
            .body(issued.toResponse(links))
    }

    @GetMapping
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun list(): List<SubscriptionResponse> = queries.list().map(Subscription::toResponse)

    @GetMapping("/{id}")
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun get(@PathVariable id: String): ResponseEntity<SubscriptionResponse> {
        val subscription = queries.get(id)
        return ResponseEntity.ok().eTag(subscription.version.toString()).body(subscription.toResponse())
    }

    @PutMapping("/{id}")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun replace(
        @PathVariable id: String,
        @RequestHeader("If-Match") ifMatch: String,
        @RequestBody request: ReplaceSubscriptionRequest,
        principal: Principal,
    ): ResponseEntity<SubscriptionResponse> {
        val version = ifMatch.removeSurrounding("\"").toLongOrNull()
            ?: throw InvalidRequest("If-Match must contain the numeric subscription version")
        val updated = commands.replace(id, version, request.toReplacement(), principal.name)
        return ResponseEntity.ok().eTag(updated.version.toString()).body(updated.toResponse())
    }

    /** Постоянная ссылка подписки: секреты хранятся зашифрованными и восстанавливаются по запросу. */
    @GetMapping("/{id}/link")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun link(@PathVariable id: String): SubscriptionLinkResponse =
        SubscriptionLinkResponse(queries.link(id))

    /** Выпускает новую ссылку и немедленно обесценивает прежнюю. */
    @PostMapping("/{id}/rotate")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun rotate(
        @PathVariable id: String,
        @RequestHeader("If-Match") ifMatch: String,
        principal: Principal,
    ): ResponseEntity<IssuedSubscriptionResponse> {
        val version = ifMatch.removeSurrounding("\"").toLongOrNull()
            ?: throw InvalidRequest("If-Match must contain the numeric subscription version")
        val issued = commands.rotate(id, version, principal.name)
        return ResponseEntity.ok().eTag(issued.subscription.version.toString()).body(issued.toResponse(links))
    }

    @PostMapping("/resolve")
    @PreAuthorize("hasAnyRole('SUBSCRIBER', 'ADMIN')")
    fun resolve(@RequestBody request: ResolveSubscriptionRequest): SubscriptionSnapshot =
        queries.resolve(request.token, request.recoveryPublicKey)
}

/** url = null, когда доставка подписками выключена или у подписки нет сохранённых секретов. */
data class SubscriptionLinkResponse(val url: String?)

data class SubscriptionKeyRequest(val id: String, val name: String, val nodeId: String, val userId: String)
data class CreateSubscriptionRequest(val name: String, val keys: List<SubscriptionKeyRequest>)
data class ReplaceSubscriptionRequest(val name: String, val state: String, val keys: List<SubscriptionKeyRequest>)
data class ResolveSubscriptionRequest(val token: String? = null, val recoveryPublicKey: String? = null)

data class SubscriptionKeyResponse(
    val id: String,
    val name: String,
    val nodeId: String,
    val userId: String,
    val position: Int,
)

data class SubscriptionResponse(
    val id: String,
    val name: String,
    val recoveryClientPublicKey: String,
    val state: String,
    val keys: List<SubscriptionKeyResponse>,
    val version: Long,
    val createdAt: Instant,
    val updatedAt: Instant,
)

data class IssuedSubscriptionResponse(
    val subscription: SubscriptionResponse,
    val token: String,
    val recoveryClientPrivateKey: String,
    /** Готовая ссылка; null, когда доставка подписками выключена. */
    val url: String?,
)

private fun CreateSubscriptionRequest.toDraft() = SubscriptionDraft(name, keys.map(SubscriptionKeyRequest::toDraft))

private fun ReplaceSubscriptionRequest.toReplacement() = SubscriptionReplacement(
    name = name,
    state = runCatching { SubscriptionState.valueOf(state.uppercase()) }
        .getOrElse { throw InvalidRequest("subscription state must be enabled, disabled, or revoked") },
    keys = keys.map(SubscriptionKeyRequest::toDraft),
)

private fun SubscriptionKeyRequest.toDraft() = SubscriptionKeyDraft(id, name, nodeId, userId)

private fun IssuedSubscription.toResponse(links: SubscriptionLinkBuilder) = IssuedSubscriptionResponse(
    subscription.toResponse(), token, recoveryClientPrivateKey,
    url = if (links.enabled) links.build(this) else null,
)

private fun Subscription.toResponse() = SubscriptionResponse(
    id = id, name = name, recoveryClientPublicKey = recoveryPublicKey,
    state = state.name.lowercase(),
    keys = keys.sortedBy { it.position }.map {
        SubscriptionKeyResponse(it.id, it.name, it.nodeId, it.userId, it.position)
    },
    version = version, createdAt = createdAt, updatedAt = updatedAt,
)
