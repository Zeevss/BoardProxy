package io.boardproxy.control.provisioning.api.rest

import io.boardproxy.control.provisioning.application.NodeInput
import io.boardproxy.control.provisioning.application.NodeService
import io.boardproxy.control.provisioning.application.Page
import io.boardproxy.control.provisioning.domain.model.CoreSettings
import io.boardproxy.control.provisioning.domain.model.ManagementSettings
import io.boardproxy.control.provisioning.domain.model.Node
import io.boardproxy.control.provisioning.domain.model.ObservabilitySettings
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.provisioning.domain.model.ServerSettings
import io.boardproxy.control.provisioning.domain.model.TransportSettings
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
import java.time.Duration
import java.time.Instant

@RestController
@RequestMapping("/api/v1/nodes")
class NodeController(private val service: NodeService) {

    @GetMapping
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun list(
        @RequestParam(required = false) query: String?,
        @RequestParam(defaultValue = "0") offset: Int,
        @RequestParam(defaultValue = "50") limit: Int,
    ): Page<NodeResponse> = service.list(query, offset, limit.coerceIn(1, MAXIMUM_PAGE)).map()

    @GetMapping("/{id}")
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun get(@PathVariable id: String): ResponseEntity<NodeResponse> = service.get(id).ok()

    @PostMapping
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun create(@RequestBody request: NodeRequest, principal: Principal): ResponseEntity<NodeResponse> {
        val node = service.create(request.toInput(), principal.name)
        return ResponseEntity.status(HttpStatus.CREATED).eTag(node.version.toString()).body(node.toResponse())
    }

    @PutMapping("/{id}")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun update(
        @PathVariable id: String,
        @RequestHeader("If-Match") ifMatch: String,
        @RequestBody request: NodeRequest,
        principal: Principal,
    ): ResponseEntity<NodeResponse> =
        service.update(id, ifMatch.version("node"), request.toInput(), principal.name).ok()

    /** Полное удаление: борды, гранты, конфигурация, снимки и телеметрия уходят каскадом. */
    @DeleteMapping("/{id}")
    @PreAuthorize("hasRole('ADMIN')")
    fun delete(
        @PathVariable id: String,
        @RequestHeader("If-Match") ifMatch: String,
        principal: Principal,
    ): ResponseEntity<Void> {
        service.delete(id, ifMatch.version("node"), principal.name)
        return ResponseEntity.noContent().build()
    }

    private fun Node.ok() = ResponseEntity.ok().eTag(version.toString()).body(toResponse())

    private fun Page<Node>.map() = Page(items.map(Node::toResponse), offset, limit, total)

    private companion object {
        const val MAXIMUM_PAGE = 200
    }
}

data class NodeRequest(
    val id: String? = null,
    val name: String,
    val state: String = "enabled",
    val settings: CoreSettingsRequest? = null,
)

/** Приватного ключа сервера здесь нет ни на входе, ни на выходе — его ведёт хаб. */
data class CoreSettingsRequest(
    val idleTimeout: String = "PT90S",
    val allowPrivateEgress: Boolean = false,
    val window: Int = 0,
    val maxFramePayload: Int = 4 shl 20,
    val streamWindow: Int = 1 shl 20,
    val maxStreamWindow: Int = 32 shl 20,
    val ackTimeout: String = "PT6S",
    val coalesceTarget: Int = 0,
    val streamIdleTimeout: String = "PT0S",
    val grpcListen: String = "unix:///run/bproxy/control.sock",
    val httpListen: String? = null,
    val observabilityEnabled: Boolean = true,
    val logLevel: String = "info",
)

data class NodeResponse(
    val id: String,
    val name: String,
    val state: String,
    val settings: CoreSettingsRequest,
    val version: Long,
    val updatedAt: Instant,
)

internal fun String.version(resource: String): Long = removeSurrounding("\"").toLongOrNull()
    ?: throw io.boardproxy.control.shared.errors.InvalidRequest(
        "If-Match must contain the numeric $resource version",
    )

internal fun String.resourceState(): ResourceState = runCatching { ResourceState.valueOf(trim().uppercase()) }
    .getOrElse {
        throw io.boardproxy.control.shared.errors.InvalidRequest("state must be enabled, disabled, or revoked")
    }

private fun NodeRequest.toInput() = NodeInput(
    id = id,
    name = name,
    state = state.resourceState(),
    // Ключ сервера сюда не попадает: при создании его выпускает хаб, при правке
    // переносит из текущей записи.
    settings = settings?.toDomain(),
)

/**
 * Ключ здесь — заглушка: [io.boardproxy.control.provisioning.application.NodeService]
 * подставляет настоящий (свежий при создании, текущий при правке) прежде, чем
 * собрать доменную ноду. Наружу приватный ключ не выходит никогда.
 */
private fun CoreSettingsRequest.toDomain() = CoreSettings(
    server = ServerSettings(PLACEHOLDER_KEY, Duration.parse(idleTimeout), allowPrivateEgress),
    transport = TransportSettings(
        window, maxFramePayload, streamWindow, maxStreamWindow,
        Duration.parse(ackTimeout), coalesceTarget, Duration.parse(streamIdleTimeout),
    ),
    management = ManagementSettings(grpcListen, httpListen),
    observability = ObservabilitySettings(observabilityEnabled, logLevel),
)

private const val PLACEHOLDER_KEY = "base64:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

private fun Node.toResponse() = NodeResponse(
    id = id,
    name = name,
    state = state.name.lowercase(),
    settings = CoreSettingsRequest(
        idleTimeout = core.server.idleTimeout.toString(),
        allowPrivateEgress = core.server.allowPrivateEgress,
        window = core.transport.window,
        maxFramePayload = core.transport.maxFramePayload,
        streamWindow = core.transport.streamWindow,
        maxStreamWindow = core.transport.maxStreamWindow,
        ackTimeout = core.transport.ackTimeout.toString(),
        coalesceTarget = core.transport.coalesceTarget,
        streamIdleTimeout = core.transport.streamIdleTimeout.toString(),
        grpcListen = core.management.grpcListen,
        httpListen = core.management.httpListen,
        observabilityEnabled = core.observability.enabled,
        logLevel = core.observability.logLevel,
    ),
    version = version,
    updatedAt = updatedAt,
)
