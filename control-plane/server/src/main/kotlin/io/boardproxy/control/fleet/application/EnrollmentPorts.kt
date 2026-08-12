package io.boardproxy.control.fleet.application

import io.boardproxy.control.fleet.domain.EnrollmentToken
import io.boardproxy.control.fleet.domain.IssuedCertificate
import java.time.Duration
import java.security.PrivateKey
import java.security.cert.X509Certificate
import io.boardproxy.control.fleet.domain.NodeCertificate
import java.time.Instant

interface EnrollmentTokenRepository {
    fun create(nodeId: String, ttl: Duration): EnrollmentToken
    fun consume(nodeId: String, plaintext: String): Boolean
}

interface CertificateAuthority {
    fun caCertificatePem(): ByteArray
    fun issueNodeCertificate(nodeId: String, csrPem: ByteArray): IssuedCertificate
}

interface ServerCertificateAuthority : CertificateAuthority {
    val caCertificate: X509Certificate
    val serverCertificate: X509Certificate
    val serverPrivateKey: PrivateKey
}

interface NodeCertificateRepository {
    fun record(nodeId: String, issued: IssuedCertificate)
    fun list(nodeId: String): List<NodeCertificate>
    fun revoke(nodeId: String, serialNumber: String, reason: String, revokedAt: Instant): Boolean
    fun active(nodeId: String, serialNumber: String, now: Instant): Boolean
    fun touch(nodeId: String, serialNumber: String, seenAt: Instant)
    fun nodeEnabled(nodeId: String): Boolean
}

fun interface NodeCertificateQueries {
    fun list(nodeId: String): List<io.boardproxy.control.fleet.domain.NodeCertificate>
}

fun interface NodeCertificateCommands {
    fun revoke(nodeId: String, serialNumber: String, reason: String, actor: String)
}

fun interface NodeConnectionPolicy {
    fun authorize(nodeId: String, certificate: X509Certificate): Boolean
}
