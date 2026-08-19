package io.boardproxy.control.subscription.infrastructure.persistence.postgres

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.module.kotlin.readValue
import io.boardproxy.control.shared.agents.Agent
import io.boardproxy.control.shared.agents.AgentCommandRepository
import io.boardproxy.control.shared.agents.AgentKind
import io.boardproxy.control.shared.agents.AgentRegistry
import io.boardproxy.control.shared.agents.AgentStatus
import io.boardproxy.control.shared.agents.AgentStatusRepository
import io.boardproxy.control.shared.persistence.toSqlTimestamp
import io.boardproxy.control.shared.security.EncryptedSecret
import io.boardproxy.control.shared.security.SecretCipher
import io.boardproxy.control.subscription.application.SubscriptionApp
import io.boardproxy.control.subscription.application.SubscriptionServiceReport
import io.boardproxy.control.subscription.application.SubscriptionServiceRepository
import io.boardproxy.control.subscription.application.SubscriptionServiceSettings
import io.boardproxy.control.subscription.application.SubscriptionServiceStatus
import io.boardproxy.control.subscription.application.SubscriptionServiceUpdate
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository
import java.sql.ResultSet
import java.time.Instant

/**
 * Настройки живут одной строкой: сервис подписок на инсталляцию ровно один.
 *
 * Наблюдаемое состояние и перезапуск хранятся не здесь, а в общих таблицах
 * агентов — там же, где состояние нод. HTTP-контракт `/poll` при этом не
 * изменился, поэтому Go-сервис править не пришлось.
 */
@Repository
class PostgresSubscriptionServiceRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val cipher: SecretCipher,
    private val json: ObjectMapper,
    private val agents: AgentRegistry,
    private val statuses: AgentStatusRepository,
    private val commands: AgentCommandRepository,
) : SubscriptionServiceRepository {

    /** Строка агента заводится лениво: до первой правки настроек её может не быть. */
    private fun agentId(): String = AGENT_ID.also {
        if (agents.find(it) == null) {
            agents.register(Agent(it, AgentKind.SUBSCRIPTION_SERVICE, "Сервис подписок"))
        }
    }

    override fun settings(): SubscriptionServiceSettings = requireNotNull(
        jdbc.query("SELECT * FROM subscription_service_settings WHERE id = true") { rs, _ -> settings(rs) }
            .firstOrNull(),
    ) { "subscription service settings row is missing" }

    override fun status(): SubscriptionServiceStatus {
        val status = statuses.find(agentId()) ?: AgentStatus(agentId = AGENT_ID)
        val delivered = commands.pending(AGENT_ID)
        return SubscriptionServiceStatus(
            lastSeenAt = status.lastReportAt,
            serviceVersion = status.agentVersion,
            appliedRevision = status.appliedRevision.takeIf { it > 0 },
            recoveryWatcherReady = status.details["recoveryWatcherReady"] as? Boolean,
            startedAt = (status.details["startedAt"] as? String)?.let(Instant::parse),
            // Ждущая команда означает, что перезапуск ещё не доставлен.
            ackedRestartNonce = if (delivered == null) restartNonce() else delivered.nonce - 1,
        )
    }

    private fun restartNonce(): Long = jdbc.queryForObject(
        "SELECT COALESCE(MAX(nonce), 0) FROM agent_commands WHERE agent_id = :agentId",
        mapOf("agentId" to AGENT_ID),
        Long::class.java,
    ) ?: 0

    override fun replace(
        update: SubscriptionServiceUpdate,
        revision: Long,
        expectedRevision: Long,
        at: Instant,
    ): Boolean = jdbc.update(
        """
        UPDATE subscription_service_settings SET
            enabled = :enabled, service_name = :serviceName, icon = :icon,
            public_url = :publicUrl, yandex_editor_url = :yandexEditorUrl,
            recovery_key_id = :recoveryKeyId, apps = CAST(:apps AS jsonb),
            revision = :revision, updated_at = :updatedAt
        WHERE id = true AND revision = :expectedRevision
        """.trimIndent(),
        mapOf(
            "enabled" to update.enabled, "serviceName" to update.serviceName, "icon" to update.icon,
            "publicUrl" to update.publicUrl, "yandexEditorUrl" to update.yandexEditorUrl,
            "recoveryKeyId" to update.recoveryKeyId, "apps" to json.writeValueAsString(update.apps),
            "revision" to revision, "expectedRevision" to expectedRevision, "updatedAt" to at.toSqlTimestamp(),
        ),
    ) == 1

    override fun recoveryPrivateKey(): String? {
        val stored = jdbc.query(
            """
            SELECT recovery_private_ciphertext, recovery_private_nonce, recovery_private_key_id
            FROM subscription_service_settings WHERE id = true
            """.trimIndent(),
        ) { rs, _ ->
            StoredKey(
                rs.getBytes("recovery_private_ciphertext"),
                rs.getBytes("recovery_private_nonce"),
                rs.getString("recovery_private_key_id"),
            )
        }.firstOrNull() ?: return null
        val ciphertext = stored.ciphertext ?: return null
        return cipher.decrypt(
            RECOVERY_CONTEXT,
            EncryptedSecret(ciphertext, requireNotNull(stored.nonce), requireNotNull(stored.keyId)),
        )
    }

    private data class StoredKey(val ciphertext: ByteArray?, val nonce: ByteArray?, val keyId: String?)

    override fun saveRecoveryKeys(privateKey: String, publicKey: String, at: Instant) {
        val encrypted = cipher.encrypt(RECOVERY_CONTEXT, privateKey)
        jdbc.update(
            """
            UPDATE subscription_service_settings SET
                recovery_private_ciphertext = :ciphertext, recovery_private_nonce = :nonce,
                recovery_private_key_id = :keyId, recovery_public_key = :publicKey, updated_at = :updatedAt
            WHERE id = true
            """.trimIndent(),
            mapOf(
                "ciphertext" to encrypted.ciphertext, "nonce" to encrypted.nonce, "keyId" to encrypted.keyId,
                "publicKey" to publicKey, "updatedAt" to at.toSqlTimestamp(),
            ),
        )
    }

    override fun bumpRestartNonce(at: Instant): Long = commands.issue(agentId(), "restart", "operator", at)

    override fun attachToken(tokenId: String?, at: Instant) {
        jdbc.update(
            "UPDATE subscription_service_settings SET token_id = :tokenId, updated_at = :updatedAt WHERE id = true",
            mapOf("tokenId" to tokenId, "updatedAt" to at.toSqlTimestamp()),
        )
    }

    override fun tokenId(): String? = jdbc.query(
        "SELECT token_id FROM subscription_service_settings WHERE id = true",
    ) { rs, _ -> rs.getString("token_id") }.firstOrNull()

    override fun recordReport(report: SubscriptionServiceReport, at: Instant) {
        statuses.record(
            AgentStatus(
                agentId = agentId(),
                appliedRevision = report.appliedRevision ?: 0,
                agentVersion = report.serviceVersion,
                lastReportAt = at,
                details = mapOf(
                    "recoveryWatcherReady" to report.recoveryWatcherReady,
                    "startedAt" to report.startedAt?.toString(),
                ),
            ),
        )
    }

    override fun markRestartDelivered(nonce: Long, at: Instant) {
        commands.markDelivered(AGENT_ID, nonce, at)
    }

    private fun settings(rs: ResultSet) = SubscriptionServiceSettings(
        enabled = rs.getBoolean("enabled"),
        serviceName = rs.getString("service_name"),
        icon = rs.getString("icon"),
        publicUrl = rs.getString("public_url"),
        yandexEditorUrl = rs.getString("yandex_editor_url"),
        recoveryKeyId = rs.getString("recovery_key_id"),
        recoveryPublicKey = rs.getString("recovery_public_key"),
        apps = json.readValue<List<SubscriptionApp>>(rs.getString("apps")),
        revision = rs.getLong("revision"),
        restartNonce = restartNonce(),
        tokenIssued = rs.getString("token_id") != null,
        updatedAt = rs.getTimestamp("updated_at").toInstant(),
    )

    private companion object {
        const val RECOVERY_CONTEXT = "subscription-service:recovery"

        /** Сервис подписок на инсталляцию один, поэтому идентификатор фиксированный. */
        const val AGENT_ID = "subscription-service"
    }
}
