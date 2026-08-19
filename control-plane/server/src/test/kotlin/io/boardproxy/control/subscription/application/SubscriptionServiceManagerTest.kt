package io.boardproxy.control.subscription.application

import io.boardproxy.control.shared.contracts.IssuedServiceToken
import io.boardproxy.control.shared.contracts.ServiceTokenIssuer
import io.boardproxy.control.shared.audit.AuditRepository
import io.boardproxy.control.shared.audit.AuditEvent
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.persistence.TransactionRunner
import java.time.Clock
import java.time.Duration
import java.time.Instant
import java.time.ZoneOffset
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

class SubscriptionServiceManagerTest {
    private val now = Instant.parse("2026-08-16T12:00:00Z")

    private fun valid() = SubscriptionServiceUpdate(
        enabled = true, serviceName = "BoardProxy", icon = "◈",
        publicUrl = "https://subscribe.example.com",
        yandexEditorUrl = "https://disk.yandex.ru/i/sheet",
        recoveryKeyId = "recovery-2026-01",
        apps = listOf(SubscriptionApp("android", "https://example.com/apk")),
    )

    @Test
    fun `enabling delivery generates the recovery key once and keeps it across edits`() {
        val fixture = Fixture()

        fixture.manager.update(valid(), 1, "operator")
        val first = fixture.repository.publicKey

        fixture.manager.update(valid().copy(serviceName = "Другое имя"), 2, "operator")

        assertNotNull(first)
        // Смена ключа обесценила бы все ранее выданные ссылки подписок.
        assertEquals(first, fixture.repository.publicKey)
    }

    @Test
    fun `enabled delivery refuses a non-HTTPS public URL`() {
        val fixture = Fixture()

        assertFailsWith<InvalidRequest> {
            fixture.manager.update(valid().copy(publicUrl = "http://subscribe.example.com"), 1, "operator")
        }
    }

    @Test
    fun `enabled delivery refuses an untrusted recovery sheet host`() {
        val fixture = Fixture()

        assertFailsWith<InvalidRequest> {
            fixture.manager.update(valid().copy(yandexEditorUrl = "https://evil.example/sheet"), 1, "operator")
        }
    }

    @Test
    fun `disabled delivery may stay incomplete`() {
        val fixture = Fixture()

        val settings = fixture.manager.update(
            SubscriptionServiceUpdate(false, "", "", "", "", "", emptyList()), 1, "operator",
        )

        assertEquals(false, settings.enabled)
    }

    @Test
    fun `a stale revision is rejected`() {
        val fixture = Fixture()

        assertFailsWith<ResourceConflict> { fixture.manager.update(valid(), 99, "operator") }
    }

    @Test
    fun `re-issuing the token revokes the previous one`() {
        val fixture = Fixture()

        val first = fixture.manager.issueToken("admin")
        val second = fixture.manager.issueToken("admin")

        assertEquals(listOf(first.id), fixture.tokens.revoked)
        assertEquals(second.id, fixture.repository.attachedToken)
    }

    @Test
    fun `poll returns nothing when the service is already current`() {
        val fixture = Fixture()
        fixture.manager.update(valid(), 1, "operator")
        val revision = fixture.manager.settings().revision

        val config = fixture.manager.poll(report(revision), since = revision)

        assertNull(config)
    }

    @Test
    fun `poll returns the config with the private key when the revision moved`() {
        val fixture = Fixture()
        fixture.manager.update(valid(), 1, "operator")

        val config = fixture.manager.poll(report(null), since = null)

        assertNotNull(config)
        assertEquals("https://subscribe.example.com", config.publicUrl)
        assertTrue(config.recoveryPrivateKey.isNotBlank())
    }

    @Test
    fun `a pending restart is delivered even when the revision matches`() {
        val fixture = Fixture()
        fixture.manager.update(valid(), 1, "operator")
        val revision = fixture.manager.settings().revision
        fixture.manager.requestRestart("operator")

        val config = fixture.manager.poll(report(revision), since = revision)

        assertNotNull(config)
        assertTrue(config.restartRequested)
    }

    @Test
    fun `a restart is delivered exactly once`() {
        val fixture = Fixture()
        fixture.manager.update(valid(), 1, "operator")
        val revision = fixture.manager.settings().revision
        fixture.manager.requestRestart("operator")
        fixture.manager.poll(report(revision), since = revision)

        val second = fixture.manager.poll(report(revision), since = revision)

        assertNull(second)
    }

    @Test
    fun `a restarted service does not receive the same restart again`() {
        val fixture = Fixture()
        fixture.manager.update(valid(), 1, "operator")
        val revision = fixture.manager.settings().revision
        fixture.manager.requestRestart("operator")
        // Сервис получил команду и перезапустился.
        assertNotNull(fixture.manager.poll(report(revision), since = revision))

        // После старта он ничего не помнит и приходит как чистый: первый запрос
        // без ревизии обязан вернуть конфиг, но уже без повторного перезапуска.
        val afterRestart = fixture.manager.poll(report(null), since = null)

        assertNotNull(afterRestart)
        assertEquals(false, afterRestart.restartRequested)
    }

    @Test
    fun `poll records what the service reported`() {
        val fixture = Fixture()
        fixture.manager.update(valid(), 1, "operator")

        fixture.manager.poll(report(null, version = "1.4.0", watcher = true), since = null)

        val status = fixture.manager.status()
        assertEquals("1.4.0", status.serviceVersion)
        assertEquals(true, status.recoveryWatcherReady)
        assertEquals(now, status.lastSeenAt)
    }

    private fun report(
        revision: Long?,
        version: String? = "1.0.0",
        watcher: Boolean? = null,
    ) = SubscriptionServiceReport(version, revision, watcher, now)

    private inner class Fixture {
        val repository = MemoryRepository()
        val tokens = RecordingTokens()
        val manager = SubscriptionServiceManager(
            repository, tokens, AuditRepository { _: AuditEvent -> },
            object : TransactionRunner {
                override fun <T> required(block: () -> T): T = block()
            },
            Clock.fixed(now, ZoneOffset.UTC),
        )
    }

    private inner class RecordingTokens : ServiceTokenIssuer {
        val revoked = mutableListOf<String>()
        private var counter = 0

        override fun issueSubscriberToken(name: String, actor: String): IssuedServiceToken {
            counter += 1
            return IssuedServiceToken("token-$counter", "bpat_secret_$counter")
        }

        override fun revoke(tokenId: String, actor: String) { revoked += tokenId }
    }

    private inner class MemoryRepository : SubscriptionServiceRepository {
        private var current = SubscriptionServiceSettings(
            enabled = false, serviceName = "BoardProxy", icon = "", publicUrl = "",
            yandexEditorUrl = "", recoveryKeyId = "", recoveryPublicKey = null,
            apps = emptyList(), revision = 1, restartNonce = 0, tokenIssued = false, updatedAt = now,
        )
        private var status = SubscriptionServiceStatus(null, null, null, null, null, 0)
        private var privateKey: String? = null
        var publicKey: String? = null
        var attachedToken: String? = null

        override fun settings() = current
        override fun status() = status

        override fun replace(
            update: SubscriptionServiceUpdate,
            revision: Long,
            expectedRevision: Long,
            at: Instant,
        ): Boolean {
            if (current.revision != expectedRevision) return false
            current = current.copy(
                enabled = update.enabled, serviceName = update.serviceName, icon = update.icon,
                publicUrl = update.publicUrl, yandexEditorUrl = update.yandexEditorUrl,
                recoveryKeyId = update.recoveryKeyId, apps = update.apps, revision = revision, updatedAt = at,
            )
            return true
        }

        override fun recoveryPrivateKey() = privateKey

        override fun saveRecoveryKeys(privateKey: String, publicKey: String, at: Instant) {
            this.privateKey = privateKey
            this.publicKey = publicKey
            current = current.copy(recoveryPublicKey = publicKey)
        }

        override fun bumpRestartNonce(at: Instant): Long {
            current = current.copy(restartNonce = current.restartNonce + 1)
            return current.restartNonce
        }

        override fun attachToken(tokenId: String?, at: Instant) {
            attachedToken = tokenId
            current = current.copy(tokenIssued = tokenId != null)
        }

        override fun tokenId() = attachedToken

        override fun recordReport(report: SubscriptionServiceReport, at: Instant) {
            status = status.copy(
                lastSeenAt = at, serviceVersion = report.serviceVersion,
                appliedRevision = report.appliedRevision,
                recoveryWatcherReady = report.recoveryWatcherReady, startedAt = report.startedAt,
            )
        }

        override fun markRestartDelivered(nonce: Long, at: Instant) {
            status = status.copy(ackedRestartNonce = nonce)
        }
    }
}
