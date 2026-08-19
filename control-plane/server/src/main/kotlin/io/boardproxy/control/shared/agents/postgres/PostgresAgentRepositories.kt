package io.boardproxy.control.shared.agents.postgres

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.shared.agents.Agent
import io.boardproxy.control.shared.agents.AgentCommand
import io.boardproxy.control.shared.agents.AgentCommandRepository
import io.boardproxy.control.shared.agents.AgentKind
import io.boardproxy.control.shared.agents.AgentRegistry
import io.boardproxy.control.shared.agents.AgentReportLog
import io.boardproxy.control.shared.agents.AgentStatus
import io.boardproxy.control.shared.agents.AgentStatusRepository
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet
import java.time.Instant

@Repository
class PostgresAgentRegistry(private val jdbc: NamedParameterJdbcTemplate) : AgentRegistry {

    override fun register(agent: Agent) {
        jdbc.update(
            """
            INSERT INTO agents (id, kind, name) VALUES (:id, :kind, :name)
            ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name
            """.trimIndent(),
            mapOf("id" to agent.id, "kind" to agent.kind.databaseValue(), "name" to agent.name),
        )
    }

    override fun find(id: String): Agent? = jdbc.query(
        "SELECT id, kind, name FROM agents WHERE id = :id",
        mapOf("id" to id),
    ) { rs, _ -> agent(rs) }.firstOrNull()

    override fun list(): List<Agent> = jdbc.query(
        "SELECT id, kind, name FROM agents ORDER BY kind, name, id",
        emptyMap<String, Any>(),
    ) { rs, _ -> agent(rs) }

    private fun agent(rs: ResultSet) =
        Agent(rs.getString("id"), AgentKind.parse(rs.getString("kind")), rs.getString("name"))
}

@Repository
class PostgresAgentStatusRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val json: ObjectMapper,
) : AgentStatusRepository {

    override fun find(agentId: String): AgentStatus? = jdbc.query(
        "$SELECT WHERE agent_id = :agentId",
        mapOf("agentId" to agentId),
    ) { rs, _ -> status(rs) }.firstOrNull()

    override fun list(): List<AgentStatus> =
        jdbc.query("$SELECT ORDER BY agent_id", emptyMap<String, Any>()) { rs, _ -> status(rs) }

    /**
     * Отчёт несёт состояние целиком, поэтому запись безусловна: устаревшего
     * кэша, который мог бы перезаписать свежий, ни у кого нет — отсюда и
     * отсутствие лиза с fencing-токеном.
     */
    override fun record(status: AgentStatus) {
        jdbc.update(
            """
            INSERT INTO agent_status (
                agent_id, boot_id, seq, applied_revision, applied_sha256, apply_error,
                agent_version, uptime_seconds, last_report_at, details
            ) VALUES (
                :agentId, :bootId, :seq, :appliedRevision, :appliedSha256, :applyError,
                :agentVersion, :uptimeSeconds, :lastReportAt, CAST(:details AS jsonb)
            )
            ON CONFLICT (agent_id) DO UPDATE SET
                boot_id = EXCLUDED.boot_id,
                seq = EXCLUDED.seq,
                applied_revision = EXCLUDED.applied_revision,
                applied_sha256 = EXCLUDED.applied_sha256,
                apply_error = EXCLUDED.apply_error,
                agent_version = EXCLUDED.agent_version,
                uptime_seconds = EXCLUDED.uptime_seconds,
                last_report_at = EXCLUDED.last_report_at,
                details = EXCLUDED.details
            """.trimIndent(),
            mapOf(
                "agentId" to status.agentId,
                "bootId" to status.bootId,
                "seq" to status.seq,
                "appliedRevision" to status.appliedRevision,
                "appliedSha256" to status.appliedSha256,
                "applyError" to status.applyError,
                "agentVersion" to status.agentVersion,
                "uptimeSeconds" to status.uptimeSeconds,
                "lastReportAt" to status.lastReportAt?.toSqlTimestamp(),
                "details" to json.writeValueAsString(status.details),
            ),
        )
    }

    @Suppress("UNCHECKED_CAST")
    private fun status(rs: ResultSet) = AgentStatus(
        agentId = rs.getString("agent_id"),
        bootId = rs.getString("boot_id"),
        seq = rs.getLong("seq"),
        appliedRevision = rs.getLong("applied_revision"),
        appliedSha256 = rs.getString("applied_sha256"),
        applyError = rs.getString("apply_error"),
        agentVersion = rs.getString("agent_version"),
        uptimeSeconds = rs.getObject("uptime_seconds")?.let { (it as Number).toLong() },
        lastReportAt = rs.getTimestamp("last_report_at")?.toInstant(),
        details = json.readValue(rs.getString("details"), Map::class.java) as Map<String, Any?>,
    )

    private companion object {
        val SELECT = """
            SELECT agent_id, boot_id, seq, applied_revision, applied_sha256, apply_error,
                   agent_version, uptime_seconds, last_report_at, details::text AS details
            FROM agent_status
        """.trimIndent()
    }
}

@Repository
class PostgresAgentCommandRepository(
    private val jdbc: NamedParameterJdbcTemplate,
) : AgentCommandRepository {

    override fun issue(agentId: String, kind: String, issuedBy: String, at: Instant): Long = requireNotNull(
        jdbc.queryForObject(
            """
            INSERT INTO agent_commands (agent_id, nonce, kind, issued_by, issued_at)
            SELECT :agentId, COALESCE(MAX(nonce), 0) + 1, :kind, :issuedBy, :issuedAt
            FROM agent_commands WHERE agent_id = :agentId
            RETURNING nonce
            """.trimIndent(),
            mapOf("agentId" to agentId, "kind" to kind, "issuedBy" to issuedBy, "issuedAt" to at.toSqlTimestamp()),
            Long::class.java,
        ),
    )

    override fun pending(agentId: String): AgentCommand? = jdbc.query(
        """
        SELECT agent_id, nonce, kind, issued_by, issued_at
        FROM agent_commands WHERE agent_id = :agentId AND delivered_at IS NULL
        ORDER BY nonce LIMIT 1
        """.trimIndent(),
        mapOf("agentId" to agentId),
    ) { rs, _ ->
        AgentCommand(
            agentId = rs.getString("agent_id"),
            nonce = rs.getLong("nonce"),
            kind = rs.getString("kind"),
            issuedBy = rs.getString("issued_by"),
            issuedAt = rs.getTimestamp("issued_at").toInstant(),
        )
    }.firstOrNull()

    override fun markDelivered(agentId: String, nonce: Long, at: Instant) {
        jdbc.update(
            "UPDATE agent_commands SET delivered_at = :at WHERE agent_id = :agentId AND nonce = :nonce",
            mapOf("agentId" to agentId, "nonce" to nonce, "at" to at.toSqlTimestamp()),
        )
    }
}

@Repository
class PostgresAgentReportLog(private val jdbc: NamedParameterJdbcTemplate) : AgentReportLog {

    /** Дедупликация одной вставкой: 0 строк означает повтор. */
    override fun claim(agentId: String, batchId: String, at: Instant): Boolean = jdbc.update(
        """
        INSERT INTO agent_reports (agent_id, batch_id, received_at)
        VALUES (:agentId, :batchId, :at)
        ON CONFLICT (agent_id, batch_id) DO NOTHING
        """.trimIndent(),
        mapOf("agentId" to agentId, "batchId" to batchId, "at" to at.toSqlTimestamp()),
    ) == 1
}
