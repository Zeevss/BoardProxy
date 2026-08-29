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
        validateHubAddress(hubUrl)
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

    /**
     * Адрес хаба уезжает в секрет и попадает ноде прямо в gRPC-клиент, а тот
     * ждёт `host:port`. Со схемой он дописывает собственный `:443` и падает с
     * «too many colons in address» — по этой ошибке оператор не поймёт, что в
     * поле оказался адрес панели вместо gRPC-листенера.
     *
     * Проверка только синтаксическая. Дозваниваться отсюда до адреса нельзя:
     * хаб не обязан видеть себя по тому же имени, по которому его видит нода,
     * и удачная проверка связи ничего бы не доказала, а неудачная — запрещала
     * бы верную конфигурацию.
     */
    private fun validateHubAddress(hubUrl: String) {
        val address = hubUrl.trim()
        if (address.contains("://")) {
            throw InvalidRequest("hubUrl must be host:port of the node gRPC listener, without a scheme")
        }
        val port = when {
            // IPv6 в квадратных скобках: двоеточий внутри много, порт — после «]».
            address.startsWith("[") -> address.substringAfterLast("]:", missingDelimiterValue = "")
            address.count { it == ':' } == 1 -> address.substringAfterLast(':')
            else -> ""
        }
        val host = address.removeSuffix(":$port")
        val number = port.toIntOrNull()
        if (host.isBlank() || number == null || number !in 1..65_535) {
            throw InvalidRequest("hubUrl must be host:port with a port between 1 and 65535")
        }
    }

    private companion object {
        val NODE_ID = Regex("^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$")
    }
}

class InvalidEnrollmentToken : RuntimeException("invalid or expired enrollment token")
