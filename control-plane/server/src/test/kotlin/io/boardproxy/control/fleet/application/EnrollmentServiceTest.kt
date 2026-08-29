package io.boardproxy.control.fleet.application

import com.fasterxml.jackson.module.kotlin.jacksonObjectMapper
import io.boardproxy.control.fleet.domain.EnrollmentToken
import io.boardproxy.control.fleet.domain.IssuedCertificate
import io.boardproxy.control.fleet.domain.NodeCertificate
import io.boardproxy.control.shared.audit.AuditRepository
import io.boardproxy.control.shared.audit.AuditEvent
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.persistence.TransactionRunner
import java.time.Clock
import java.time.Duration
import java.time.Instant
import java.time.ZoneOffset
import java.util.Base64
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class EnrollmentServiceTest {
    @Test
    fun `bootstrap secret remains compatible with node agent JSON contract`() {
        val expiresAt = Instant.parse("2026-03-01T10:15:00Z")
        val tokens = Tokens(EnrollmentToken("one-time", expiresAt))
        val audit = mutableListOf<AuditEvent>()
        val service = EnrollmentService(
            tokens, Authority, Certificates, jacksonObjectMapper(), AuditRepository(audit::add), DirectTransaction,
            Clock.fixed(Instant.parse("2026-03-01T10:00:00Z"), ZoneOffset.UTC),
        )

        val (encoded, actualExpiry) = service.issueBootstrap(
            "node-1", "hub:8443", Duration.ofMinutes(15), "operator",
        )
        val json = Base64.getUrlDecoder().decode(encoded).toString(Charsets.UTF_8)

        assertEquals(expiresAt, actualExpiry)
        assertTrue(json.contains("\"node_id\":\"node-1\""))
        assertTrue(json.contains("\"hub_url\":\"hub:8443\""))
        assertTrue(json.contains("\"enrollment_token\":\"one-time\""))
        assertFalse(json.contains("nodeId"))
        assertEquals("enrollment-token.created", audit.single().action)
        assertEquals("operator", audit.single().actor)
    }

    /**
     * Адрес хаба уходит ноде как цель gRPC. Панель какое-то время подставляла
     * туда свой origin, и нода падала с «too many colons in address» — ошибка,
     * по которой причину не найти. Отсекаем такое значение на выдаче.
     */
    @Test
    fun `bootstrap secret rejects a panel origin in place of the gRPC target`() {
        val service = service()

        for (address in listOf("http://127.0.0.1:8080", "https://hub.example.net", "hub.example.net", "hub:", "hub:0")) {
            assertFailsWith<InvalidRequest>(address) {
                service.issueBootstrap("node-1", address, Duration.ofMinutes(15), "operator")
            }
        }
    }

    @Test
    fun `bootstrap secret accepts host and port forms`() {
        val service = service()

        for (address in listOf("hub:8443", "hub.example.net:8443", "127.0.0.1:8443", "[::1]:8443")) {
            service.issueBootstrap("node-1", address, Duration.ofMinutes(15), "operator")
        }
    }

    private fun service() = EnrollmentService(
        Tokens(EnrollmentToken("one-time", Instant.parse("2026-03-01T10:15:00Z"))),
        Authority, Certificates, jacksonObjectMapper(), AuditRepository { }, DirectTransaction,
        Clock.fixed(Instant.parse("2026-03-01T10:00:00Z"), ZoneOffset.UTC),
    )

    private class Tokens(private val issued: EnrollmentToken) : EnrollmentTokenRepository {
        override fun create(nodeId: String, ttl: Duration): EnrollmentToken = issued
        override fun consume(nodeId: String, plaintext: String): Boolean = true
    }

    private object Authority : CertificateAuthority {
        override fun caCertificatePem(): ByteArray = "ca-pem".toByteArray()
        override fun issueNodeCertificate(nodeId: String, csrPem: ByteArray): IssuedCertificate = error("not used")
    }

    private object Certificates : NodeCertificateRepository {
        override fun record(nodeId: String, issued: IssuedCertificate) = Unit
        override fun list(nodeId: String): List<NodeCertificate> = emptyList()
        override fun revoke(nodeId: String, serialNumber: String, reason: String, revokedAt: Instant) = false
        override fun active(nodeId: String, serialNumber: String, now: Instant) = true
        override fun touch(nodeId: String, serialNumber: String, seenAt: Instant) = Unit
        override fun nodeEnabled(nodeId: String) = true
    }

    private object DirectTransaction : TransactionRunner {
        override fun <T> required(block: () -> T): T = block()
    }
}
