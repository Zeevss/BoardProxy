package io.boardproxy.control.access.application

import io.boardproxy.control.access.domain.AccessRole
import io.boardproxy.control.access.domain.PanelAdministrator
import io.boardproxy.control.access.domain.PanelSession
import io.boardproxy.control.audit.application.AuditRepository
import io.boardproxy.control.audit.domain.AuditEvent
import io.boardproxy.control.shared.errors.AuthenticationFailed
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.persistence.TransactionRunner
import java.security.MessageDigest
import java.time.Clock
import java.time.Duration
import java.time.Instant
import java.time.ZoneOffset
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class PanelAuthServiceTest {
    private val now = Instant.parse("2026-08-16T08:00:00Z")

    @Test
    fun `first visit requires setup and setup returns an admin session`() {
        val fixture = Fixture()

        assertTrue(fixture.service.status().setupRequired)
        val issued = fixture.service.setup(" Admin.User ", "correct horse battery")

        assertFalse(fixture.service.status().setupRequired)
        assertEquals("admin.user", issued.username)
        assertTrue(issued.token.startsWith("bps_"))
        assertEquals(AccessRole.ADMIN, fixture.service.authenticate(issued.token)?.role)
        assertEquals("admin.user", fixture.service.authenticate(issued.token)?.name)
        assertEquals("panel-administrator.created", fixture.audit.single().action)
        assertEquals(1, fixture.transactions)
    }

    @Test
    fun `setup is single use and validates credentials`() {
        val fixture = Fixture()

        assertFailsWith<InvalidRequest> { fixture.service.setup("x", "correct horse battery") }
        assertFailsWith<InvalidRequest> { fixture.service.setup("admin", "short") }
        fixture.service.setup("admin", "correct horse battery")
        assertFailsWith<ResourceConflict> { fixture.service.setup("other", "another good password") }
    }

    @Test
    fun `login uses the password hash and logout revokes only the current session`() {
        val fixture = Fixture()
        val first = fixture.service.setup("admin", "correct horse battery")

        assertFailsWith<AuthenticationFailed> { fixture.service.login("admin", "wrong password") }
        val second = fixture.service.login("ADMIN", "correct horse battery")
        assertEquals("admin", fixture.service.authenticate(second.token)?.name)

        fixture.service.logout(second.token)
        assertNull(fixture.service.authenticate(second.token))
        assertEquals("admin", fixture.service.authenticate(first.token)?.name)
    }

    @Test
    fun `expired session cannot authenticate`() {
        val fixture = Fixture()
        val issued = fixture.service.setup("admin", "correct horse battery")
        fixture.clock = Clock.fixed(now.plus(Duration.ofHours(13)), ZoneOffset.UTC)

        assertNull(fixture.service.authenticate(issued.token))
    }

    private inner class Fixture {
        var administrator: PanelAdministrator? = null
        val sessions = mutableListOf<PanelSession>()
        val audit = mutableListOf<AuditEvent>()
        var transactions = 0
        var clock: Clock = Clock.fixed(now, ZoneOffset.UTC)
        private val repository = object : PanelAccessRepository {
            override fun administrator(): PanelAdministrator? = administrator

            override fun createAdministrator(administrator: PanelAdministrator): Boolean {
                if (this@Fixture.administrator != null) return false
                this@Fixture.administrator = administrator
                return true
            }

            override fun createSession(session: PanelSession) {
                sessions += session
            }

            override fun findActiveSessionByHash(tokenHash: String, now: Instant): PanelSession? =
                sessions.firstOrNull { it.tokenHash == tokenHash && it.revokedAt == null && it.expiresAt > now }

            override fun touchSession(id: String, usedAt: Instant) {
                val index = sessions.indexOfFirst { it.id == id }
                if (index >= 0) sessions[index] = sessions[index].copy(lastUsedAt = usedAt)
            }

            override fun revokeSessionByHash(tokenHash: String, revokedAt: Instant): Boolean {
                val index = sessions.indexOfFirst { it.tokenHash == tokenHash && it.revokedAt == null }
                if (index < 0) return false
                sessions[index] = sessions[index].copy(revokedAt = revokedAt)
                return true
            }
        }
        private val passwordHasher = object : PasswordHasher {
            override fun hash(password: String): String = "hash:${password.sha256()}"
            override fun matches(password: String, encoded: String): Boolean = encoded == hash(password)
        }
        val service: PanelAuthService
            get() = PanelAuthService(
                repository,
                passwordHasher,
                AuditRepository(audit::add),
                object : TransactionRunner {
                    override fun <T> required(block: () -> T): T {
                        transactions++
                        return block()
                    }
                },
                clock,
                Duration.ofHours(12),
            )

        private fun String.sha256(): String = MessageDigest.getInstance("SHA-256")
            .digest(toByteArray()).joinToString("") { "%02x".format(it) }
    }
}
