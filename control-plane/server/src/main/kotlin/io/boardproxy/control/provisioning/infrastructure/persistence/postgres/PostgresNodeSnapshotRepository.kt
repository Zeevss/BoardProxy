package io.boardproxy.control.provisioning.infrastructure.persistence.postgres

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.provisioning.application.NodeSnapshotMetadata
import io.boardproxy.control.provisioning.application.NodeSnapshotRepository
import io.boardproxy.control.provisioning.domain.model.Board
import io.boardproxy.control.provisioning.domain.model.Node
import io.boardproxy.control.provisioning.domain.model.NodeState
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.provisioning.domain.model.User
import io.boardproxy.control.provisioning.domain.model.UserPlacement
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import io.boardproxy.control.shared.security.EncryptedSecret
import io.boardproxy.control.shared.security.SecretCipher
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.time.Instant

/**
 * История ноды: снимок владеемого состояния перед каждым изменением.
 *
 * Payload шифруется целиком, поэтому приватные ключи внутри него лежат открытым
 * текстом — как и в прежних снимках каталога. Ровно одна строка на изменение:
 * прежняя реализация писала две, текущее состояние и новое.
 */
@Repository
class PostgresNodeSnapshotRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val json: ObjectMapper,
    private val secrets: SecretCipher,
) : NodeSnapshotRepository {

    override fun save(state: NodeState, cause: String, actor: String, at: Instant): Long {
        val seq = (
            jdbc.queryForObject(
                "SELECT COALESCE(MAX(seq), 0) FROM node_config_snapshots WHERE node_id = :nodeId",
                mapOf("nodeId" to state.node.id),
                Long::class.java,
            ) ?: 0
            ) + 1
        val encrypted = secrets.encrypt(
            snapshotContext(state.node.id),
            json.writeValueAsString(StoredNodeState.from(state)),
        )
        jdbc.update(
            """
            INSERT INTO node_config_snapshots (
                node_id, seq, payload_ciphertext, payload_nonce, payload_key_id,
                cause, actor, created_at
            ) VALUES (
                :nodeId, :seq, :ciphertext, :nonce, :keyId, :cause, :actor, :createdAt
            )
            """.trimIndent(),
            mapOf(
                "nodeId" to state.node.id, "seq" to seq,
                "ciphertext" to encrypted.ciphertext, "nonce" to encrypted.nonce,
                "keyId" to encrypted.keyId, "cause" to cause, "actor" to actor,
                "createdAt" to at.toSqlTimestamp(),
            ),
        )
        return seq
    }

    override fun find(nodeId: String, seq: Long): NodeState? = jdbc.query(
        """
        SELECT payload_ciphertext, payload_nonce, payload_key_id
        FROM node_config_snapshots WHERE node_id = :nodeId AND seq = :seq
        """.trimIndent(),
        mapOf("nodeId" to nodeId, "seq" to seq),
    ) { rs, _ ->
        val payload = secrets.decrypt(
            snapshotContext(nodeId),
            EncryptedSecret(
                rs.getBytes("payload_ciphertext"),
                rs.getBytes("payload_nonce"),
                rs.getString("payload_key_id"),
            ),
        )
        json.readValue(payload, StoredNodeState::class.java).toDomain()
    }.firstOrNull()

    override fun list(nodeId: String, offset: Int, limit: Int): List<NodeSnapshotMetadata> = jdbc.query(
        """
        SELECT node_id, seq, cause, actor, created_at
        FROM node_config_snapshots WHERE node_id = :nodeId
        ORDER BY seq DESC OFFSET :offset LIMIT :limit
        """.trimIndent(),
        mapOf("nodeId" to nodeId, "offset" to offset, "limit" to limit),
    ) { rs, _ ->
        NodeSnapshotMetadata(
            nodeId = rs.getString("node_id"),
            seq = rs.getLong("seq"),
            cause = rs.getString("cause"),
            actor = rs.getString("actor"),
            createdAt = rs.getTimestamp("created_at").toInstant(),
        )
    }

    override fun count(nodeId: String): Long = jdbc.queryForObject(
        "SELECT count(*) FROM node_config_snapshots WHERE node_id = :nodeId",
        mapOf("nodeId" to nodeId),
        Long::class.java,
    ) ?: 0
}

private data class StoredNodeState(
    val node: StoredNode,
    val boards: List<StoredBoard>,
    val placements: List<StoredPlacement>,
) {
    fun toDomain() = NodeState(
        node = node.toDomain(),
        boards = boards.map { it.toDomain(node.id) },
        placements = placements.map { it.toDomain() },
    )

    companion object {
        fun from(state: NodeState) = StoredNodeState(
            node = StoredNode.from(state.node),
            boards = state.boards.map(StoredBoard::from),
            placements = state.placements.map(StoredPlacement::from),
        )
    }
}

private data class StoredNode(
    val id: String,
    val name: String,
    val state: String,
    val privateKey: String,
    val core: StoredCoreSettings,
    val version: Long,
    val updatedAt: Instant,
) {
    fun toDomain() = Node(id, name, ResourceState.valueOf(state), core.toDomain(privateKey), version, updatedAt)

    companion object {
        fun from(node: Node) = StoredNode(
            node.id, node.name, node.state.name, node.core.server.privateKey,
            StoredCoreSettings.from(node.core), node.version, node.updatedAt,
        )
    }
}

private data class StoredBoard(
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
) {
    fun toDomain(nodeId: String) = Board(
        nodeId, id, name, hash, hubSlide, apiBase, guestName,
        ResourceState.valueOf(state), maxLanes, version, updatedAt,
    )

    companion object {
        fun from(board: Board) = StoredBoard(
            board.id, board.name, board.hash, board.hubSlide, board.apiBase, board.guestName,
            board.state.name, board.maxLanes, board.version, board.updatedAt,
        )
    }
}

private data class StoredPlacement(
    val id: String,
    val name: String,
    val privateKey: String?,
    val publicKey: String?,
    val state: String,
    val maxSessions: Int,
    val maxLanes: Int,
    val version: Long,
    val updatedAt: Instant,
    val boardIds: Set<String>,
) {
    fun toDomain() = UserPlacement(
        User(id, name, privateKey, publicKey, ResourceState.valueOf(state), maxSessions, maxLanes, version, updatedAt),
        boardIds,
    )

    companion object {
        fun from(placement: UserPlacement) = StoredPlacement(
            placement.user.id, placement.user.name, placement.user.privateKey, placement.user.publicKey,
            placement.user.state.name, placement.user.maxSessions, placement.user.maxLanes,
            placement.user.version, placement.user.updatedAt, placement.boardIds,
        )
    }
}
