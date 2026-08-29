package io.boardproxy.control.fleet.infrastructure.config

import com.fasterxml.jackson.module.kotlin.jacksonObjectMapper
import io.boardproxy.control.fleet.application.NodeCertificateRepository
import io.boardproxy.control.fleet.domain.IssuedCertificate
import io.boardproxy.control.fleet.domain.NodeCertificate
import io.boardproxy.control.fleet.infrastructure.pki.FileCertificateAuthority
import io.boardproxy.control.shared.security.AesGcmSecretCipher
import org.bouncycastle.asn1.x500.X500Name
import org.bouncycastle.openssl.jcajce.JcaPEMWriter
import org.bouncycastle.operator.jcajce.JcaContentSignerBuilder
import org.bouncycastle.pkcs.jcajce.JcaPKCS10CertificationRequestBuilder
import java.io.StringWriter
import java.security.KeyPairGenerator
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate
import java.security.spec.ECGenParameterSpec
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import java.util.Base64
import kotlin.io.path.createTempDirectory
import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class FleetConnectionPolicyTest {
    @Test
    fun `неизвестный ca-valid сертификат не добавляется в allowlist`() {
        val now = Instant.parse("2026-03-01T10:00:00Z")
        val clock = Clock.fixed(now, ZoneOffset.UTC)
        val key = Base64.getEncoder().encodeToString(ByteArray(32) { 9 })
        val authority = FileCertificateAuthority(
            createTempDirectory("boardproxy-policy-test"), listOf("localhost"),
            AesGcmSecretCipher(key, "test-key"), jacksonObjectMapper(), clock,
        )
        val issued = authority.issueNodeCertificate("node-1", csr("node-1"))
        val certificate = CertificateFactory.getInstance("X.509")
            .generateCertificate(issued.certificatePem.inputStream()) as X509Certificate
        val certificates = Certificates()
        val policy = FleetConfiguration().nodeConnectionPolicy(certificates, clock)

        assertFalse(policy.authorize("node-1", certificate))
        assertFalse(certificates.recorded, "входящее соединение не должно регистрировать сертификат")

        certificates.allowed += certificate.serialNumber.toString(16)
        assertTrue(policy.authorize("node-1", certificate))
        assertTrue(certificates.touched)
    }

    private class Certificates : NodeCertificateRepository {
        val allowed = mutableSetOf<String>()
        var recorded = false
        var touched = false
        override fun record(nodeId: String, issued: IssuedCertificate) { recorded = true }
        override fun active(nodeId: String, serialNumber: String, now: Instant) = serialNumber in allowed
        override fun list(nodeId: String) = emptyList<NodeCertificate>()
        override fun revoke(nodeId: String, serialNumber: String, reason: String, revokedAt: Instant) = true
        override fun touch(nodeId: String, serialNumber: String, seenAt: Instant) { touched = true }
        override fun nodeEnabled(nodeId: String) = true
    }

    private fun csr(commonName: String): ByteArray {
        val keys = KeyPairGenerator.getInstance("EC").run {
            initialize(ECGenParameterSpec("secp256r1"))
            generateKeyPair()
        }
        val request = JcaPKCS10CertificationRequestBuilder(X500Name("CN=$commonName"), keys.public)
            .build(JcaContentSignerBuilder("SHA256withECDSA").build(keys.private))
        return StringWriter().also { output -> JcaPEMWriter(output).use { it.writeObject(request) } }
            .toString().toByteArray()
    }
}
