package io.boardproxy.control.subscription.infrastructure.persistence.postgres

import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.module.kotlin.readValue
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

/** Настройки живут одной строкой: сервис подписок на инсталляцию ровно один. */
@Repository
class PostgresSubscriptionServiceRepository(
    private val jdbc: NamedParameterJdbcTemplate,
    private val cipher: SecretCipher,
    private val json: ObjectMapper,
) : SubscriptionServiceRepository {

    override fun settings(): SubscriptionServiceSettings = requireNotNull(
        jdbc.query("SELECT * FROM subscription_service_settings WHERE id = true") { rs, _ -> settings(rs) }
            .firstOrNull(),
    ) { "subscription service settings row is missing" }

    override fun status(): SubscriptionServiceStatus = requireNotNull(
        jdbc.query("SELECT * FROM subscription_service_status WHERE id = true") { rs, _ ->
            SubscriptionServiceStatus(
                lastSeenAt = rs.getTimestamp("last_seen_at")?.toInstant(),
                serviceVersion = rs.getString("service_version"),
                appliedRevision = rs.getObject("applied_revision") as? Long,
                recoveryWatcherReady = rs.getObject("recovery_watcher_ready") as? Boolean,
                startedAt = rs.getTimestamp("started_at")?.toInstant(),
                ackedRestartNonce = rs.getLong("acked_restart_nonce"),
            )
        }.firstOrNull(),
    ) { "subscription service status row is missing" }

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

    override fun bumpRestartNonce(at: Instant): Long = requireNotNull(
        jdbc.queryForObject(
            """
            UPDATE subscription_service_settings SET restart_nonce = restart_nonce + 1, updated_at = :updatedAt
            WHERE id = true RETURNING restart_nonce
            """.trimIndent(),
            mapOf("updatedAt" to at.toSqlTimestamp()),
            Long::class.java,
        ),
    )

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
        jdbc.update(
            """
            UPDATE subscription_service_status SET
                last_seen_at = :at, service_version = :version, applied_revision = :applied,
                recovery_watcher_ready = :watcher, started_at = :startedAt
            WHERE id = true
            """.trimIndent(),
            mapOf(
                "at" to at.toSqlTimestamp(), "version" to report.serviceVersion,
                "applied" to report.appliedRevision, "watcher" to report.recoveryWatcherReady,
                "startedAt" to report.startedAt?.toSqlTimestamp(),
            ),
        )
    }

    override fun markRestartDelivered(nonce: Long, at: Instant) {
        jdbc.update(
            "UPDATE subscription_service_status SET acked_restart_nonce = :nonce WHERE id = true",
            mapOf("nonce" to nonce),
        )
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
        restartNonce = rs.getLong("restart_nonce"),
        tokenIssued = rs.getString("token_id") != null,
        updatedAt = rs.getTimestamp("updated_at").toInstant(),
    )

    private companion object {
        const val RECOVERY_CONTEXT = "subscription-service:recovery"
    }
}
