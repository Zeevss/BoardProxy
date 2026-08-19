package io.boardproxy.control.fleet.application

import io.boardproxy.control.shared.audit.AuditRepository
import io.boardproxy.control.shared.audit.AuditEvent
import io.boardproxy.control.fleet.domain.IssuedCertificate
import io.boardproxy.control.fleet.domain.NodeCertificate
import io.boardproxy.control.shared.persistence.TransactionRunner
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import kotlin.test.Test
import kotlin.test.assertEquals

class NodeCertificateServiceTest {
    @Test
    fun `revocation is audited with authenticated actor and reason`() {
        val now = Instant.parse("2026-08-12T12:00:00Z")
        val audit = mutableListOf<AuditEvent>()
        val repository = Certificates()
        val service = NodeCertificateService(
            repository, AuditRepository { audit += it }, DirectTransactions, Clock.fixed(now, ZoneOffset.UTC), { "audit-1" },
        )

        service.revoke("node-1", "serial-1", " emergency ", "admin")

        assertEquals("admin", audit.single().actor)
        assertEquals("node.certificate.revoked", audit.single().action)
        assertEquals("emergency", audit.single().details["reason"])
        assertEquals(now, repository.revokedAt)
    }

    private class Certificates : NodeCertificateRepository {
        var revokedAt: Instant? = null
        override fun record(nodeId: String, issued: IssuedCertificate) = Unit
        override fun active(nodeId: String, serialNumber: String, now: Instant) = true
        override fun list(nodeId: String) = emptyList<NodeCertificate>()
        override fun revoke(nodeId: String, serialNumber: String, reason: String, revokedAt: Instant): Boolean { this.revokedAt = revokedAt; return true }
        override fun touch(nodeId: String, serialNumber: String, seenAt: Instant) = Unit
        override fun nodeEnabled(nodeId: String) = true
    }

    private object DirectTransactions : TransactionRunner {
        override fun <T> required(block: () -> T): T = block()
    }
}
