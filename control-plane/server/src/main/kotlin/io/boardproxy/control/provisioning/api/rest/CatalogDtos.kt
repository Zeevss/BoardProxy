package io.boardproxy.control.provisioning.api.rest

import com.fasterxml.jackson.annotation.JsonProperty
import io.boardproxy.control.provisioning.domain.model.AssignedUser
import io.boardproxy.control.provisioning.domain.model.Board
import io.boardproxy.control.provisioning.domain.model.Catalog
import io.boardproxy.control.provisioning.domain.model.CoreSettings
import io.boardproxy.control.provisioning.domain.model.ManagementSettings
import io.boardproxy.control.provisioning.domain.model.Node
import io.boardproxy.control.provisioning.domain.model.NodeAssignment
import io.boardproxy.control.provisioning.domain.model.ObservabilitySettings
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.provisioning.domain.model.ServerSettings
import io.boardproxy.control.provisioning.domain.model.TransportSettings
import io.boardproxy.control.provisioning.domain.model.User
import io.boardproxy.control.shared.errors.InvalidRequest
import java.time.Duration
import java.time.Instant

data class CatalogWriteRequest(
    val node: NodeWriteRequest,
    val boards: List<BoardWriteRequest>,
    val users: List<UserWriteRequest>,
    val assignment: AssignmentWriteRequest,
)

data class NodeWriteRequest(
    val id: String,
    val name: String,
    val state: String = "enabled",
    val core: CoreSettingsWriteRequest,
)

data class CoreSettingsWriteRequest(
    @JsonProperty(access = JsonProperty.Access.WRITE_ONLY)
    val serverPrivateKey: String? = null,
    val idleTimeout: String = "PT1M30S",
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

data class BoardWriteRequest(
    val id: String,
    val name: String,
    val hash: String,
    val hubSlide: String? = null,
    val apiBase: String? = null,
    val guestName: String? = null,
    val state: String = "enabled",
    val maxLanes: Int = 1,
)

data class UserWriteRequest(
    val id: String,
    val name: String,
    @JsonProperty(access = JsonProperty.Access.WRITE_ONLY)
    val privateKey: String? = null,
    val publicKey: String? = null,
    val state: String = "enabled",
    val maxSessions: Int = 0,
    val maxLanes: Int = 1,
)

data class AssignmentWriteRequest(
    val boardIds: List<String>,
    val users: List<AssignedUserWriteRequest>,
)

data class AssignedUserWriteRequest(val userId: String, val boardIds: List<String>)

data class CatalogResponse(
    val node: NodeResponse,
    val boards: List<BoardResponse>,
    val users: List<UserResponse>,
    val assignment: AssignmentResponse,
    val version: Long,
    val updatedAt: Instant,
)

data class NodeResponse(
    val id: String,
    val name: String,
    val state: String,
    val core: SafeCoreSettingsResponse,
    val version: Long,
    val updatedAt: Instant,
)

data class SafeCoreSettingsResponse(
    val idleTimeout: String,
    val allowPrivateEgress: Boolean,
    val transport: TransportSettingsResponse,
    val management: ManagementSettingsResponse,
    val observability: ObservabilitySettingsResponse,
)

data class TransportSettingsResponse(
    val window: Int,
    val maxFramePayload: Int,
    val streamWindow: Int,
    val maxStreamWindow: Int,
    val ackTimeout: String,
    val coalesceTarget: Int,
    val streamIdleTimeout: String,
)

data class ManagementSettingsResponse(val grpcListen: String, val httpListen: String?)

data class ObservabilitySettingsResponse(val enabled: Boolean, val logLevel: String)

data class BoardResponse(
    val id: String,
    val name: String,
    val hash: String,
    val hubSlide: String?,
    val apiBase: String?,
    val guestName: String?,
    val state: String,
    val maxLanes: Int,
    val version: Long,
    val updatedAt: Instant,
)

data class UserResponse(
    val id: String,
    val name: String,
    val publicKey: String?,
    val credentialType: String,
    val state: String,
    val maxSessions: Int,
    val maxLanes: Int,
    val version: Long,
    val updatedAt: Instant,
)

data class AssignmentResponse(
    val boardIds: List<String>,
    val users: List<AssignedUserWriteRequest>,
    val version: Long,
    val updatedAt: Instant,
)

data class CatalogMutationResponse(
    val catalog: CatalogResponse,
    val desiredRevision: Long,
    val configSha256: String,
    val configChanged: Boolean,
)

internal fun CatalogWriteRequest.toNewCatalog(now: Instant) = toCatalog(now, null)

internal fun CatalogWriteRequest.toReplacement(now: Instant, current: Catalog): Catalog = toCatalog(now, current)

private fun CatalogWriteRequest.toCatalog(now: Instant, current: Catalog?): Catalog {
    val nodeVersion = current?.node?.version?.plus(1) ?: 1
    val boardVersions = current?.boards?.associate { it.id to it.version }.orEmpty()
    val userVersions = current?.users?.associate { it.id to it.version }.orEmpty()
    return Catalog(
        node = Node(
            id = node.id, name = node.name, state = node.state.resourceState(),
            core = node.core.toDomain(current?.node?.core?.server?.privateKey),
            version = nodeVersion,
            updatedAt = now,
        ),
        boards = boards.map { value ->
            Board(
                id = value.id, name = value.name, hash = value.hash,
                hubSlide = value.hubSlide, apiBase = value.apiBase, guestName = value.guestName,
                state = value.state.resourceState(), maxLanes = value.maxLanes,
                version = boardVersions[value.id]?.plus(1) ?: 1, updatedAt = now,
            )
        },
        users = users.map { value ->
            val previous = current?.users?.firstOrNull { it.id == value.id }
            User(
                id = value.id, name = value.name,
                privateKey = value.privateKey ?: previous?.privateKey,
                publicKey = value.publicKey ?: if (value.privateKey == null) previous?.publicKey else null,
                state = value.state.resourceState(), maxSessions = value.maxSessions, maxLanes = value.maxLanes,
                version = userVersions[value.id]?.plus(1) ?: 1, updatedAt = now,
            )
        },
        assignment = NodeAssignment(
            nodeId = node.id, boardIds = assignment.boardIds,
            users = assignment.users.map { AssignedUser(it.userId, it.boardIds) },
            version = current?.assignment?.version?.plus(1) ?: 1, updatedAt = now,
        ),
        version = current?.version?.plus(1) ?: 1,
        updatedAt = now,
    )
}

private fun CoreSettingsWriteRequest.toDomain(previousServerPrivateKey: String?) = CoreSettings(
    server = ServerSettings(
        privateKey = serverPrivateKey ?: previousServerPrivateKey
            ?: throw InvalidRequest("serverPrivateKey is required when creating a catalog"),
        idleTimeout = Duration.parse(idleTimeout),
        allowPrivateEgress = allowPrivateEgress,
    ),
    transport = TransportSettings(
        window, maxFramePayload, streamWindow, maxStreamWindow, Duration.parse(ackTimeout),
        coalesceTarget, Duration.parse(streamIdleTimeout),
    ),
    management = ManagementSettings(grpcListen, httpListen),
    observability = ObservabilitySettings(observabilityEnabled, logLevel),
)

internal fun Catalog.toResponse() = CatalogResponse(
    node = NodeResponse(
        id = node.id, name = node.name, state = node.state.apiValue(),
        core = SafeCoreSettingsResponse(
            idleTimeout = node.core.server.idleTimeout.toString(),
            allowPrivateEgress = node.core.server.allowPrivateEgress,
            transport = node.core.transport.let {
                TransportSettingsResponse(
                    it.window, it.maxFramePayload, it.streamWindow, it.maxStreamWindow,
                    it.ackTimeout.toString(), it.coalesceTarget, it.streamIdleTimeout.toString(),
                )
            },
            management = node.core.management.let { ManagementSettingsResponse(it.grpcListen, it.httpListen) },
            observability = node.core.observability.let { ObservabilitySettingsResponse(it.enabled, it.logLevel) },
        ),
        version = node.version, updatedAt = node.updatedAt,
    ),
    boards = boards.map {
        BoardResponse(
            it.id, it.name, it.hash, it.hubSlide, it.apiBase, it.guestName,
            it.state.apiValue(), it.maxLanes, it.version, it.updatedAt,
        )
    },
    users = users.map {
        UserResponse(
            id = it.id, name = it.name, publicKey = it.publicKey,
            credentialType = if (it.privateKey != null) "private" else "public",
            state = it.state.apiValue(), maxSessions = it.maxSessions, maxLanes = it.maxLanes,
            version = it.version, updatedAt = it.updatedAt,
        )
    },
    assignment = AssignmentResponse(
        boardIds = assignment.boardIds,
        users = assignment.users.map { AssignedUserWriteRequest(it.userId, it.boardIds) },
        version = assignment.version,
        updatedAt = assignment.updatedAt,
    ),
    version = version,
    updatedAt = updatedAt,
)

private fun String.resourceState() = ResourceState.valueOf(trim().uppercase())
private fun ResourceState.apiValue() = name.lowercase()
