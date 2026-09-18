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
import kotlin.test.assertTrue

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

    /**
     * Имя, по которому нода дозванивается, обязано попасть в SAN.
     *
     * Сохранённый список имён писался в файл, но никогда не сверялся с
     * настройкой: правка `CONTROL_GRPC_SERVER_NAMES` на работающей установке
     * молча ничего не меняла. Обнаруживалось это только на ноде — отказом TLS.
     */
    @Test
    fun `server certificate is reissued when the configured names change`() {
        val directory = createTempDirectory("boardproxy-pki-names")
        val cipher = AesGcmSecretCipher(Base64.getEncoder().encodeToString(ByteArray(32) { 9 }), "test-key")
        val clock = Clock.fixed(Instant.parse("2026-03-01T10:00:00Z"), ZoneOffset.UTC)

        val first = FileCertificateAuthority(
            directory, listOf("hub", "localhost"), cipher, jacksonObjectMapper(), clock,
        )
        assertFalse(
            subjectNames(first.serverCertificate).contains("node.example.net"),
            "имени ещё нет в сертификате — иначе проверка ничего не покажет",
        )

        val second = FileCertificateAuthority(
            directory, listOf("hub", "localhost", "node.example.net"), cipher, jacksonObjectMapper(), clock,
        )

        assertTrue(
            subjectNames(second.serverCertificate).contains("node.example.net"),
            "новое имя обязано попасть в SAN, иначе нода не пройдёт проверку TLS",
        )
        // Удостоверяющий центр остаётся прежним: его смена обесценила бы
        // сертификаты всех уже зачисленных нод разом.
        assertEquals(first.caCertificate, second.caCertificate)
        second.serverCertificate.verify(second.caCertificate.publicKey)
    }

    /** Неизменный список не должен приводить к перевыпуску на каждом старте. */
    @Test
    fun `server certificate survives a restart with the same names`() {
        val directory = createTempDirectory("boardproxy-pki-stable")
        val cipher = AesGcmSecretCipher(Base64.getEncoder().encodeToString(ByteArray(32) { 9 }), "test-key")
        val clock = Clock.fixed(Instant.parse("2026-03-01T10:00:00Z"), ZoneOffset.UTC)
        val names = listOf("hub", "localhost")

        val first = FileCertificateAuthority(directory, names, cipher, jacksonObjectMapper(), clock)
        val second = FileCertificateAuthority(directory, names, cipher, jacksonObjectMapper(), clock)

        assertEquals(first.serverCertificate, second.serverCertificate)
    }

    private fun subjectNames(certificate: X509Certificate): List<String> =
        certificate.subjectAlternativeNames.orEmpty().map { it[1].toString() }

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
