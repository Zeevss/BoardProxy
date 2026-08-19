package io.boardproxy.control.fleet.application

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.shared.audit.AuditRepository
import io.boardproxy.control.shared.audit.AuditEvent
import io.boardproxy.control.fleet.domain.BootstrapSecret
import io.boardproxy.control.fleet.domain.IssuedCertificate
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.persistence.TransactionRunner
import java.time.Duration
import java.time.Clock
import java.util.Base64
import java.util.UUID

class EnrollmentService(
    private val tokens: EnrollmentTokenRepository,
    private val authority: CertificateAuthority,
    private val certificates: NodeCertificateRepository,
    private val json: ObjectMapper,
    private val audit: AuditRepository,
    private val transactions: TransactionRunner,
    private val clock: Clock,
    private val nextId: () -> String = { UUID.randomUUID().toString() },
) {
    fun issueBootstrap(nodeId: String, hubUrl: String, ttl: Duration, actor: String): Pair<String, java.time.Instant> {
        validateNodeId(nodeId)
        if (hubUrl.isBlank() || actor.isBlank() || ttl.isZero || ttl.isNegative || ttl > Duration.ofHours(24)) {
            throw InvalidRequest("hubUrl and ttl between 1 second and 24 hours are required")
        }
        val now = clock.instant()
        val token = transactions.required {
            tokens.create(nodeId, ttl).also {
                audit.append(
                    AuditEvent(
                        id = nextId(), nodeId = nodeId, actor = actor,
                        action = "enrollment-token.created", resourceType = "node",
                        resourceId = nodeId, resourceVersion = 0,
                        details = mapOf("expiresAt" to it.expiresAt.toString()), occurredAt = now,
                    ),
                )
            }
        }
        val payload = BootstrapSecret(
            nodeId = nodeId,
            hubUrl = hubUrl,
            enrollmentToken = token.plaintext,
            caCertificatePem = authority.caCertificatePem().toString(Charsets.UTF_8),
        )
        val encoded = Base64.getUrlEncoder().withoutPadding().encodeToString(json.writeValueAsBytes(payload))
        return encoded to token.expiresAt
    }

    fun enroll(nodeId: String, token: String, csrPem: ByteArray): IssuedCertificate {
        validateNodeId(nodeId)
        if (token.isBlank() || csrPem.isEmpty()) throw InvalidRequest("nodeId, token and csrPem are required")
        return transactions.required {
            if (!tokens.consume(nodeId, token)) throw InvalidEnrollmentToken()
            authority.issueNodeCertificate(nodeId, csrPem).also { certificates.record(nodeId, it) }
        }
    }

    fun renew(nodeId: String, csrPem: ByteArray): IssuedCertificate {
        validateNodeId(nodeId)
        if (csrPem.isEmpty()) throw InvalidRequest("csrPem is required")
        return authority.issueNodeCertificate(nodeId, csrPem).also { certificates.record(nodeId, it) }
    }

    private fun validateNodeId(nodeId: String) {
        if (!NODE_ID.matches(nodeId)) throw InvalidRequest("invalid node id")
    }

    private companion object {
        val NODE_ID = Regex("^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$")
    }
}

class InvalidEnrollmentToken : RuntimeException("invalid or expired enrollment token")
