package io.boardproxy.control.provisioning.infrastructure.persistence.postgres

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.provisioning.application.CatalogRepository
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
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.security.EncryptedSecret
import io.boardproxy.control.shared.security.SecretCipher
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.dao.DataIntegrityViolationException
import org.springframework.jdbc.core.namedparam.MapSqlParameterSource
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet
import java.time.Duration
import java.time.Instant

@Repository
class PostgresCatalogRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val json: ObjectMapper,
    private val secrets: SecretCipher,
) : CatalogRepository {
    override fun find(nodeId: String): Catalog? {
        val node = jdbc.query(
            """
            SELECT id, name, state, core_settings::text, server_key_ciphertext,
                   server_key_nonce, server_key_key_id, resource_version,
                   catalog_version, updated_at, catalog_updated_at
            FROM nodes WHERE id = :nodeId
            """.trimIndent(),
            mapOf("nodeId" to nodeId),
        ) { rs, _ -> nodeRow(rs) }.firstOrNull() ?: return null

        val boards = jdbc.query(
            """
            SELECT id, name, board_hash, hub_slide, api_base, guest_name, state,
                   max_lanes, resource_version, updated_at
            FROM boards WHERE node_id = :nodeId ORDER BY id
            """.trimIndent(),
            mapOf("nodeId" to nodeId),
        ) { rs, _ -> boardRow(rs) }
        val users = jdbc.query(
            """
            SELECT id, name, private_key_ciphertext, private_key_nonce,
                   private_key_key_id, public_key, state, max_sessions,
                   max_lanes, resource_version, updated_at
            FROM users WHERE node_id = :nodeId ORDER BY id
            """.trimIndent(),
            mapOf("nodeId" to nodeId),
        ) { rs, _ -> userRow(nodeId, rs) }
        val assignment = assignment(nodeId)

        return Catalog(
            node = Node(
                id = node.id, name = node.name, state = node.state,
                core = node.core, version = node.resourceVersion, updatedAt = node.updatedAt,
            ),
            boards = boards,
            users = users,
            assignment = assignment,
            version = node.catalogVersion,
            updatedAt = node.catalogUpdatedAt,
        )
    }

    override fun search(query: String?, offset: Int, limit: Int): List<Catalog> {
        val normalized = query?.trim()?.takeIf { it.isNotEmpty() }
        val filter = if (normalized == null) "" else "WHERE id ILIKE '%' || :query || '%' OR name ILIKE '%' || :query || '%'"
        val ids = jdbc.queryForList(
            """
            SELECT id FROM nodes
            $filter
            ORDER BY name, id
            OFFSET :offset LIMIT :limit
            """.trimIndent(),
            mapOf("query" to normalized.orEmpty(), "offset" to offset, "limit" to limit),
            String::class.java,
        )
        return ids.mapNotNull(::find)
    }

    override fun count(query: String?): Long {
        val normalized = query?.trim()?.takeIf { it.isNotEmpty() }
        val filter = if (normalized == null) "" else "WHERE id ILIKE '%' || :query || '%' OR name ILIKE '%' || :query || '%'"
        return requireNotNull(
            jdbc.queryForObject(
                """
                SELECT COUNT(*) FROM nodes
                $filter
                """.trimIndent(),
                mapOf("query" to normalized.orEmpty()),
                Long::class.java,
            ),
        )
    }

    override fun create(catalog: Catalog) {
        try {
            insertNode(catalog)
            insertResources(catalog)
            insertAssignment(catalog.assignment)
        } catch (error: DataIntegrityViolationException) {
            throw ResourceConflict("catalog ${catalog.node.id} conflicts with persisted data")
        }
    }

    override fun replace(catalog: Catalog, expectedVersion: Long): Boolean {
        val serverKey = secrets.encrypt(serverKeyContext(catalog.node.id), catalog.node.core.server.privateKey)
        val updated = jdbc.update(
            """
            UPDATE nodes
            SET name = :name, state = :state, core_settings = CAST(:core AS jsonb),
                server_key_ciphertext = :ciphertext, server_key_nonce = :nonce,
                server_key_key_id = :keyId, resource_version = :resourceVersion,
                catalog_version = :catalogVersion, updated_at = :updatedAt,
                catalog_updated_at = :catalogUpdatedAt
            WHERE id = :id AND catalog_version = :expectedVersion
            """.trimIndent(),
            nodeParameters(catalog, serverKey).addValue("expectedVersion", expectedVersion),
        )
        if (updated != 1) return false

        jdbc.update("DELETE FROM node_user_boards WHERE node_id = :nodeId", mapOf("nodeId" to catalog.node.id))
        jdbc.update("DELETE FROM node_users WHERE node_id = :nodeId", mapOf("nodeId" to catalog.node.id))
        jdbc.update("DELETE FROM node_boards WHERE node_id = :nodeId", mapOf("nodeId" to catalog.node.id))
        jdbc.update("DELETE FROM users WHERE node_id = :nodeId", mapOf("nodeId" to catalog.node.id))
        jdbc.update("DELETE FROM boards WHERE node_id = :nodeId", mapOf("nodeId" to catalog.node.id))
        jdbc.update("DELETE FROM assignment_versions WHERE node_id = :nodeId", mapOf("nodeId" to catalog.node.id))
        insertResources(catalog)
        insertAssignment(catalog.assignment)
        return true
    }

    private fun insertNode(catalog: Catalog) {
        val serverKey = secrets.encrypt(serverKeyContext(catalog.node.id), catalog.node.core.server.privateKey)
        jdbc.update(
            """
            INSERT INTO nodes (
                id, name, state, core_settings, server_key_ciphertext,
                server_key_nonce, server_key_key_id, resource_version,
                catalog_version, updated_at, catalog_updated_at
            ) VALUES (
                :id, :name, :state, CAST(:core AS jsonb), :ciphertext,
                :nonce, :keyId, :resourceVersion, :catalogVersion,
                :updatedAt, :catalogUpdatedAt
            )
            """.trimIndent(),
            nodeParameters(catalog, serverKey),
        )
    }

    private fun nodeParameters(catalog: Catalog, serverKey: EncryptedSecret) = MapSqlParameterSource()
        .addValue("id", catalog.node.id)
        .addValue("name", catalog.node.name)
        .addValue("state", catalog.node.state.databaseValue())
        .addValue("core", json.writeValueAsString(StoredCoreSettings.from(catalog.node.core)))
        .addValue("ciphertext", serverKey.ciphertext)
        .addValue("nonce", serverKey.nonce)
        .addValue("keyId", serverKey.keyId)
        .addValue("resourceVersion", catalog.node.version)
        .addValue("catalogVersion", catalog.version)
        .addValue("updatedAt", catalog.node.updatedAt.toSqlTimestamp())
        .addValue("catalogUpdatedAt", catalog.updatedAt.toSqlTimestamp())

    private fun insertResources(catalog: Catalog) {
        catalog.boards.forEach { board ->
            jdbc.update(
                """
                INSERT INTO boards (
                    node_id, id, name, board_hash, hub_slide, api_base, guest_name,
                    state, max_lanes, resource_version, updated_at
                ) VALUES (
                    :nodeId, :id, :name, :hash, :hubSlide, :apiBase, :guestName,
                    :state, :maxLanes, :version, :updatedAt
                )
                """.trimIndent(),
                mapOf(
                    "nodeId" to catalog.node.id, "id" to board.id, "name" to board.name,
                    "hash" to board.hash, "hubSlide" to board.hubSlide, "apiBase" to board.apiBase,
                    "guestName" to board.guestName, "state" to board.state.databaseValue(),
                    "maxLanes" to board.maxLanes, "version" to board.version,
                    "updatedAt" to board.updatedAt.toSqlTimestamp(),
                ),
            )
        }
        catalog.users.forEach { user -> insertUser(catalog.node.id, user) }
    }

    private fun insertUser(nodeId: String, user: User) {
        val encrypted = user.privateKey?.let { secrets.encrypt(userKeyContext(nodeId, user.id), it) }
        jdbc.update(
            """
            INSERT INTO users (
                node_id, id, name, private_key_ciphertext, private_key_nonce,
                private_key_key_id, public_key, identity_fingerprint, state,
                max_sessions, max_lanes, resource_version, updated_at
            ) VALUES (
                :nodeId, :id, :name, :ciphertext, :nonce, :keyId, :publicKey,
                :fingerprint, :state, :maxSessions, :maxLanes, :version, :updatedAt
            )
            """.trimIndent(),
            mapOf(
                "nodeId" to nodeId, "id" to user.id, "name" to user.name,
                "ciphertext" to encrypted?.ciphertext, "nonce" to encrypted?.nonce,
                "keyId" to encrypted?.keyId, "publicKey" to user.publicKey,
                "fingerprint" to user.identityFingerprint(), "state" to user.state.databaseValue(),
                "maxSessions" to user.maxSessions, "maxLanes" to user.maxLanes,
                "version" to user.version, "updatedAt" to user.updatedAt.toSqlTimestamp(),
            ),
        )
    }

    private fun insertAssignment(assignment: NodeAssignment) {
        jdbc.update(
            "INSERT INTO assignment_versions (node_id, resource_version, updated_at) VALUES (:nodeId, :version, :updatedAt)",
            mapOf(
                "nodeId" to assignment.nodeId, "version" to assignment.version,
                "updatedAt" to assignment.updatedAt.toSqlTimestamp(),
            ),
        )
        assignment.boardIds.forEach { boardId ->
            jdbc.update(
                "INSERT INTO node_boards (node_id, board_id) VALUES (:nodeId, :boardId)",
                mapOf("nodeId" to assignment.nodeId, "boardId" to boardId),
            )
        }
        assignment.users.forEach { assigned ->
            jdbc.update(
                "INSERT INTO node_users (node_id, user_id) VALUES (:nodeId, :userId)",
                mapOf("nodeId" to assignment.nodeId, "userId" to assigned.userId),
            )
            assigned.boardIds.forEach { boardId ->
                jdbc.update(
                    """
                    INSERT INTO node_user_boards (node_id, user_id, board_id)
                    VALUES (:nodeId, :userId, :boardId)
                    """.trimIndent(),
                    mapOf("nodeId" to assignment.nodeId, "userId" to assigned.userId, "boardId" to boardId),
                )
            }
        }
    }

    private fun assignment(nodeId: String): NodeAssignment {
        val version = jdbc.query(
            "SELECT resource_version, updated_at FROM assignment_versions WHERE node_id = :nodeId",
            mapOf("nodeId" to nodeId),
        ) { rs, _ -> rs.getLong("resource_version") to rs.getTimestamp("updated_at").toInstant() }.single()
        val boardIds = jdbc.queryForList(
            "SELECT board_id FROM node_boards WHERE node_id = :nodeId ORDER BY board_id",
            mapOf("nodeId" to nodeId),
            String::class.java,
        )
        val assignedUsers = jdbc.queryForList(
            "SELECT user_id FROM node_users WHERE node_id = :nodeId ORDER BY user_id",
            mapOf("nodeId" to nodeId),
            String::class.java,
        ).map { userId ->
            val userBoards = jdbc.queryForList(
                """
                SELECT board_id FROM node_user_boards
                WHERE node_id = :nodeId AND user_id = :userId ORDER BY board_id
                """.trimIndent(),
                mapOf("nodeId" to nodeId, "userId" to userId),
                String::class.java,
            )
            AssignedUser(userId, userBoards)
        }
        return NodeAssignment(nodeId, boardIds, assignedUsers, version.first, version.second)
    }

    private fun nodeRow(rs: ResultSet): NodeRow {
        val stored = json.readValue(rs.getString("core_settings"), StoredCoreSettings::class.java)
        val key = secrets.decrypt(
            serverKeyContext(rs.getString("id")),
            EncryptedSecret(
                rs.getBytes("server_key_ciphertext"), rs.getBytes("server_key_nonce"),
                rs.getString("server_key_key_id"),
            ),
        )
        return NodeRow(
            id = rs.getString("id"), name = rs.getString("name"),
            state = rs.getString("state").resourceState(), core = stored.toDomain(key),
            resourceVersion = rs.getLong("resource_version"), catalogVersion = rs.getLong("catalog_version"),
            updatedAt = rs.getTimestamp("updated_at").toInstant(),
            catalogUpdatedAt = rs.getTimestamp("catalog_updated_at").toInstant(),
        )
    }

    private fun boardRow(rs: ResultSet) = Board(
        id = rs.getString("id"), name = rs.getString("name"), hash = rs.getString("board_hash"),
        hubSlide = rs.getString("hub_slide"), apiBase = rs.getString("api_base"),
        guestName = rs.getString("guest_name"), state = rs.getString("state").resourceState(),
        maxLanes = rs.getInt("max_lanes"), version = rs.getLong("resource_version"),
        updatedAt = rs.getTimestamp("updated_at").toInstant(),
    )

    private fun userRow(nodeId: String, rs: ResultSet): User {
        val ciphertext = rs.getBytes("private_key_ciphertext")
        val privateKey = ciphertext?.let {
            secrets.decrypt(
                userKeyContext(nodeId, rs.getString("id")),
                EncryptedSecret(it, rs.getBytes("private_key_nonce"), rs.getString("private_key_key_id")),
            )
        }
        return User(
            id = rs.getString("id"), name = rs.getString("name"), privateKey = privateKey,
            publicKey = rs.getString("public_key"), state = rs.getString("state").resourceState(),
            maxSessions = rs.getInt("max_sessions"), maxLanes = rs.getInt("max_lanes"),
            version = rs.getLong("resource_version"), updatedAt = rs.getTimestamp("updated_at").toInstant(),
        )
    }
}

private data class NodeRow(
    val id: String,
    val name: String,
    val state: ResourceState,
    val core: CoreSettings,
    val resourceVersion: Long,
    val catalogVersion: Long,
    val updatedAt: Instant,
    val catalogUpdatedAt: Instant,
)

private data class StoredCoreSettings(
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

private fun ResourceState.databaseValue() = name.lowercase()
private fun String.resourceState() = ResourceState.valueOf(uppercase())
private fun serverKeyContext(nodeId: String) = "catalog:$nodeId:server-key"
private fun userKeyContext(nodeId: String, userId: String) = "catalog:$nodeId:user:$userId:private-key"
