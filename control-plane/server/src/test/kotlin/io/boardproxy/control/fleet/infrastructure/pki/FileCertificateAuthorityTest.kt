package io.boardproxy.control.fleet.infrastructure.pki

import com.fasterxml.jackson.module.kotlin.jacksonObjectMapper
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
import kotlin.io.path.readText
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse

class FileCertificateAuthorityTest {
    @Test
    fun `authority encrypts private material and signs node CSR`() {
        val directory = createTempDirectory("boardproxy-pki-test")
        val clock = Clock.fixed(Instant.parse("2026-03-01T10:00:00Z"), ZoneOffset.UTC)
        val key = Base64.getEncoder().encodeToString(ByteArray(32) { 9 })
        val authority = FileCertificateAuthority(
            directory, listOf("hub", "localhost"), AesGcmSecretCipher(key, "test-key"),
            jacksonObjectMapper(), clock,
        )

        val issued = authority.issueNodeCertificate("node-1", csr("node-1"))
        val certificate = CertificateFactory.getInstance("X.509")
            .generateCertificate(issued.certificatePem.inputStream()) as X509Certificate

        assertEquals("CN=node-1", certificate.subjectX500Principal.name)
        certificate.verify(authority.caCertificate.publicKey)
        assertFalse(directory.resolve("authority.json").readText().contains("PRIVATE KEY"))
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
