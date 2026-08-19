package io.boardproxy.control.provisioning.infrastructure.persistence.postgres

import io.boardproxy.control.provisioning.domain.model.Board
import io.boardproxy.control.provisioning.domain.model.CoreSettings
import io.boardproxy.control.provisioning.domain.model.ManagementSettings
import io.boardproxy.control.provisioning.domain.model.ObservabilitySettings
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.provisioning.domain.model.ServerSettings
import io.boardproxy.control.provisioning.domain.model.TransportSettings
import java.sql.ResultSet
import java.time.Duration

internal fun ResourceState.databaseValue() = name.lowercase()

internal fun String.resourceState() = ResourceState.valueOf(uppercase())

/**
 * Контексты шифрования. У пользователя больше нет ноды в идентичности, поэтому
 * ключ шифруется один раз на пользователя, а не по копии на каждую ноду.
 */
internal fun serverKeyContext(nodeId: String) = "node:$nodeId:server-key"

internal fun userKeyContext(userId: String) = "user:$userId:private-key"

internal fun snapshotContext(nodeId: String) = "node:$nodeId:snapshot"

internal fun configContext(nodeId: String) = "node:$nodeId:desired-config"

internal fun boardRow(rs: ResultSet) = Board(
    nodeId = rs.getString("node_id"),
    id = rs.getString("id"),
    name = rs.getString("name"),
    hash = rs.getString("board_hash"),
    hubSlide = rs.getString("hub_slide"),
    apiBase = rs.getString("api_base"),
    guestName = rs.getString("guest_name"),
    state = rs.getString("state").resourceState(),
    maxLanes = rs.getInt("max_lanes"),
    version = rs.getLong("resource_version"),
    updatedAt = rs.getTimestamp("updated_at").toInstant(),
)

/**
 * Настройки ядра в том виде, в каком лежат в jsonb. Приватный ключ сервера сюда
 * не попадает: он хранится отдельными зашифрованными колонками.
 */
internal data class StoredCoreSettings(
    val idleTimeout: String,
    val allowPrivateEgress: Boolean,
    val window: Int,
    val maxFramePayload: Int,
    val streamWindow: Int,
    val maxStreamWindow: Int,
    val ackTimeout: String,
    val coalesceTarget: Int,
    val streamIdleTimeout: String,
    val grpcListen: String,
    val httpListen: String?,
    val observabilityEnabled: Boolean,
    val logLevel: String,
) {
    fun toDomain(privateKey: String) = CoreSettings(
        server = ServerSettings(privateKey, Duration.parse(idleTimeout), allowPrivateEgress),
        transport = TransportSettings(
            window, maxFramePayload, streamWindow, maxStreamWindow, Duration.parse(ackTimeout),
            coalesceTarget, Duration.parse(streamIdleTimeout),
        ),
        management = ManagementSettings(grpcListen, httpListen),
        observability = ObservabilitySettings(observabilityEnabled, logLevel),
    )

    companion object {
        fun from(core: CoreSettings) = StoredCoreSettings(
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
        )
    }
}
