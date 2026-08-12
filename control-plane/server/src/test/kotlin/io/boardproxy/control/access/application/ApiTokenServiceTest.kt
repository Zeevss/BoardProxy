package io.boardproxy.control.access.application

import io.boardproxy.control.access.domain.AccessRole
import io.boardproxy.control.access.domain.ApiToken
import io.boardproxy.control.audit.application.AuditRepository
import io.boardproxy.control.audit.domain.AuditEvent
import io.boardproxy.control.shared.persistence.TransactionRunner
import java.time.Clock
import java.time.Duration
import java.time.Instant
import java.time.ZoneOffset
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotEquals
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

class ApiTokenServiceTest {
    private val now = Instant.parse("2026-08-12T10:00:00Z")

    @Test
    fun `bootstrap token authenticates as admin without persistence`() {
        val fixture = Fixture(bootstrap = "bootstrap-secret")

        val principal = fixture.service.authenticate("bootstrap-secret")

        assertEquals("bootstrap-admin", principal?.name)
        assertEquals(AccessRole.ADMIN, principal?.role)
        assertNull(fixture.service.authenticate("wrong"))
    }

    @Test
    fun `issued token is returned once while only hash is persisted`() {
        val fixture = Fixture()

        val issued = fixture.service.issue("panel", AccessRole.OPERATOR, Duration.ofHours(1), "admin")

        assertTrue(issued.secret.startsWith("bpat_"))
        assertNotEquals(issued.secret, fixture.tokens.single().tokenHash)
        assertEquals(64, fixture.tokens.single().tokenHash.length)
        assertEquals(AccessRole.OPERATOR, fixture.service.authenticate(issued.secret)?.role)
        assertEquals("api-token.created", fixture.audit.single().action)
        assertEquals(1, fixture.transactions)
    }

    @Test
    fun `revoked and expired tokens cannot authenticate`() {
        val fixture = Fixture()
        val revoked = fixture.service.issue("revoked", AccessRole.VIEWER, null, "admin")
        fixture.service.revoke(revoked.token.id, "admin")
        val expired = fixture.service.issue("expired", AccessRole.VIEWER, Duration.ofSeconds(1), "admin")
        fixture.clock = Clock.fixed(now.plusSeconds(2), ZoneOffset.UTC)

        assertNull(fixture.service.authenticate(revoked.secret))
        assertNull(fixture.service.authenticate(expired.secret))
        assertTrue(fixture.audit.any { it.action == "api-token.revoked" })
    }

    private inner class Fixture(private val bootstrap: String = "") {
        val tokens = mutableListOf<ApiToken>()
        val audit = mutableListOf<AuditEvent>()
        var transactions = 0
        var clock: Clock = Clock.fixed(now, ZoneOffset.UTC)
        private val repository = object : ApiTokenRepository {
            override fun create(token: ApiToken) {
                tokens += token
            }

            override fun findActiveByHash(tokenHash: String, now: Instant): ApiToken? = tokens.firstOrNull {
                it.tokenHash == tokenHash && it.revokedAt == null && (it.expiresAt == null || it.expiresAt > now)
            }

            override fun list(): List<ApiToken> = tokens.toList()

            override fun revoke(id: String, revokedAt: Instant): Boolean {
                val index = tokens.indexOfFirst { it.id == id && it.revokedAt == null }
                if (index < 0) return false
                tokens[index] = tokens[index].copy(revokedAt = revokedAt)
                return true
            }

            override fun touch(id: String, usedAt: Instant) {
                val index = tokens.indexOfFirst { it.id == id }
                if (index >= 0) tokens[index] = tokens[index].copy(lastUsedAt = usedAt)
            }
        }
        val service: ApiTokenService
            get() = ApiTokenService(
                repository, AuditRepository(audit::add),
                object : TransactionRunner {
                    override fun <T> required(block: () -> T): T {
                        transactions++
                        return block()
                    }
                },
                clock, bootstrap,
            )
    }
}
