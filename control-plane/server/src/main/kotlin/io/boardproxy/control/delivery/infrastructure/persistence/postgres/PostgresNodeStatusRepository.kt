package io.boardproxy.control.delivery.infrastructure.persistence.postgres

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.delivery.application.NodeStatusRepository
import io.boardproxy.control.delivery.domain.ApplyOutcome
import io.boardproxy.control.delivery.domain.NodeStatus
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet

@Repository
class PostgresNodeStatusRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val json: ObjectMapper,
) : NodeStatusRepository {
    override fun find(nodeId: String): NodeStatus? = jdbc.query(
        """
        SELECT node_id, connected, boot_id, agent_version, core_version,
               core_running, core_ready, desired_revision, applied_revision,
               config_sha256, last_error, last_seen, last_apply::text, fencing_token, projection_version
        FROM node_status WHERE node_id = :nodeId
        """.trimIndent(),
        mapOf("nodeId" to nodeId),
    ) { rs, _ -> map(rs) }.firstOrNull()

    override fun save(status: NodeStatus, expectedVersion: Long): Boolean {
        if (expectedVersion == 0L) {
            return jdbc.update(
                """
                INSERT INTO node_status (
                    node_id, connected, boot_id, agent_version, core_version,
                    core_running, core_ready, desired_revision, applied_revision,
                    config_sha256, last_error, last_seen, last_apply, fencing_token, projection_version
                ) VALUES (
                    :nodeId, :connected, :bootId, :agentVersion, :coreVersion,
                    :coreRunning, :coreReady, :desiredRevision, :appliedRevision,
                    :configSha256, :lastError, :lastSeen, CAST(:lastApply AS jsonb), :fencingToken, :version
                ) ON CONFLICT (node_id) DO NOTHING
                """.trimIndent(),
                parameters(status),
            ) == 1
        }
        return jdbc.update(
            """
            UPDATE node_status SET
                connected = :connected, boot_id = :bootId, agent_version = :agentVersion,
                core_version = :coreVersion, core_running = :coreRunning, core_ready = :coreReady,
                desired_revision = :desiredRevision, applied_revision = :appliedRevision,
                config_sha256 = :configSha256, last_error = :lastError, last_seen = :lastSeen,
                last_apply = CAST(:lastApply AS jsonb), fencing_token = :fencingToken,
                projection_version = :version
            WHERE node_id = :nodeId AND projection_version = :expectedVersion
              AND fencing_token <= :fencingToken
            """.trimIndent(),
            parameters(status) + ("expectedVersion" to expectedVersion),
        ) == 1
    }

    private fun parameters(status: NodeStatus): Map<String, Any?> = mapOf(
        "nodeId" to status.nodeId, "connected" to status.connected, "bootId" to status.bootId,
        "agentVersion" to status.agentVersion, "coreVersion" to status.coreVersion,
        "coreRunning" to status.coreRunning, "coreReady" to status.coreReady,
        "desiredRevision" to status.desiredRevision, "appliedRevision" to status.appliedRevision,
        "configSha256" to status.configSha256, "lastError" to status.lastError,
        "lastSeen" to status.lastSeen?.toSqlTimestamp(),
        "lastApply" to status.lastApply?.let(json::writeValueAsString),
        "fencingToken" to status.fencingToken, "version" to status.version,
    )

    private fun map(rs: ResultSet) = NodeStatus(
        nodeId = rs.getString("node_id"), connected = rs.getBoolean("connected"),
        bootId = rs.getString("boot_id"), agentVersion = rs.getString("agent_version"),
        coreVersion = rs.getString("core_version"), coreRunning = rs.getBoolean("core_running"),
        coreReady = rs.getBoolean("core_ready"), desiredRevision = rs.getLong("desired_revision"),
        appliedRevision = rs.getLong("applied_revision"), configSha256 = rs.getString("config_sha256"),
        lastError = rs.getString("last_error"), lastSeen = rs.getTimestamp("last_seen")?.toInstant(),
        lastApply = rs.getString("last_apply")?.let { json.readValue(it, ApplyOutcome::class.java) },
        fencingToken = rs.getLong("fencing_token"),
        version = rs.getLong("projection_version"),
    )
}
