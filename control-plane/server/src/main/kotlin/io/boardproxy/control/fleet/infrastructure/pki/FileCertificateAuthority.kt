package io.boardproxy.control.fleet.infrastructure.pki

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.fleet.application.ServerCertificateAuthority
import io.boardproxy.control.fleet.domain.IssuedCertificate
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.security.EncryptedSecret
import io.boardproxy.control.shared.security.SecretCipher
import org.bouncycastle.asn1.x500.X500Name
import org.bouncycastle.asn1.x500.style.BCStyle
import org.bouncycastle.asn1.x509.BasicConstraints
import org.bouncycastle.asn1.x509.ExtendedKeyUsage
import org.bouncycastle.asn1.x509.Extension
import org.bouncycastle.asn1.x509.GeneralName
import org.bouncycastle.asn1.x509.GeneralNames
import org.bouncycastle.asn1.x509.KeyPurposeId
import org.bouncycastle.asn1.x509.KeyUsage
import org.bouncycastle.cert.jcajce.JcaX509CertificateConverter
import org.bouncycastle.cert.jcajce.JcaX509v3CertificateBuilder
import org.bouncycastle.openssl.PEMParser
import org.bouncycastle.openssl.jcajce.JcaPEMKeyConverter
import org.bouncycastle.openssl.jcajce.JcaPEMWriter
import org.bouncycastle.operator.jcajce.JcaContentSignerBuilder
import org.bouncycastle.operator.jcajce.JcaContentVerifierProviderBuilder
import org.bouncycastle.pkcs.PKCS10CertificationRequest
import org.bouncycastle.util.io.pem.PemObject
import org.bouncycastle.util.io.pem.PemWriter
import java.io.StringReader
import java.io.StringWriter
import java.math.BigInteger
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.StandardCopyOption
import java.nio.file.attribute.PosixFilePermissions
import java.security.KeyFactory
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.PrivateKey
import java.security.SecureRandom
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate
import java.security.spec.ECGenParameterSpec
import java.security.spec.PKCS8EncodedKeySpec
import java.time.Clock
import java.time.Duration
import java.time.Instant
import java.util.Base64
import java.util.Date

class FileCertificateAuthority(
    directory: Path,
    private val serverNames: List<String>,
    private val secrets: SecretCipher,
    private val json: ObjectMapper,
    private val clock: Clock,
) : ServerCertificateAuthority {
    private val materialFile = directory.resolve("authority.json")
    private val material: Material

    override val caCertificate: X509Certificate
    override val serverCertificate: X509Certificate
    override val serverPrivateKey: PrivateKey
    private val caPrivateKey: PrivateKey

    init {
        require(serverNames.isNotEmpty()) { "at least one gRPC server name is required" }
        Files.createDirectories(directory)
        secureDirectory(directory)
        material = loadOrCreate()
        caCertificate = parseCertificate(material.caCertificatePem)
        serverCertificate = parseCertificate(material.serverCertificatePem)
        caPrivateKey = parsePrivateKey(decrypt("pki:ca-private-key", material.caPrivateKey))
        serverPrivateKey = parsePrivateKey(decrypt("pki:server-private-key", material.serverPrivateKey))
    }

    override fun caCertificatePem(): ByteArray = material.caCertificatePem.toByteArray(Charsets.UTF_8)

    override fun issueNodeCertificate(nodeId: String, csrPem: ByteArray): IssuedCertificate {
        val csr = runCatching {
            PEMParser(StringReader(csrPem.toString(Charsets.UTF_8))).use { it.readObject() as PKCS10CertificationRequest }
        }.getOrElse { throw InvalidRequest("invalid CSR PEM") }
        val validSignature = runCatching {
            csr.isSignatureValid(JcaContentVerifierProviderBuilder().build(csr.subjectPublicKeyInfo))
        }.getOrDefault(false)
        if (!validSignature) throw InvalidRequest("invalid CSR signature")
        val commonName = csr.subject.getRDNs(BCStyle.CN).singleOrNull()?.first?.value?.toString()
        if (commonName != nodeId) throw InvalidRequest("CSR common name must equal node id")

        val now = clock.instant()
        val expiresAt = now.plus(NODE_VALIDITY)
        val publicKey = JcaPEMKeyConverter().getPublicKey(csr.subjectPublicKeyInfo)
        val serial = serial()
        val builder = JcaX509v3CertificateBuilder(
            caCertificate,
            serial,
            Date.from(now.minus(Duration.ofMinutes(1))),
            Date.from(expiresAt),
            csr.subject,
            publicKey,
        )
            .addExtension(Extension.basicConstraints, true, BasicConstraints(false))
            .addExtension(Extension.keyUsage, true, KeyUsage(KeyUsage.digitalSignature))
            .addExtension(Extension.extendedKeyUsage, false, ExtendedKeyUsage(KeyPurposeId.id_kp_clientAuth))
        val certificate = JcaX509CertificateConverter().getCertificate(
            builder.build(JcaContentSignerBuilder("SHA256withECDSA").build(caPrivateKey)),
        )
        certificate.verify(caCertificate.publicKey)
        return IssuedCertificate(
            serial.toString(16), pem(certificate).toByteArray(Charsets.UTF_8), caCertificatePem(), now, expiresAt,
        )
    }

    private fun loadOrCreate(): Material {
        if (Files.exists(materialFile)) return json.readValue(Files.readAllBytes(materialFile), Material::class.java)
        val now = clock.instant()
        val caKeys = keyPair()
        val caName = X500Name("CN=BoardProxy Control Plane CA")
        val ca = JcaX509CertificateConverter().getCertificate(
            JcaX509v3CertificateBuilder(
                caName, serial(), Date.from(now.minus(Duration.ofMinutes(1))), Date.from(now.plus(CA_VALIDITY)),
                caName, caKeys.public,
            )
                .addExtension(Extension.basicConstraints, true, BasicConstraints(true))
                .addExtension(
                    Extension.keyUsage, true,
                    KeyUsage(KeyUsage.keyCertSign or KeyUsage.cRLSign or KeyUsage.digitalSignature),
                )
                .build(JcaContentSignerBuilder("SHA256withECDSA").build(caKeys.private)),
        )
        val serverKeys = keyPair()
        val server = serverCertificate(ca, caKeys.private, serverKeys, now)
        val value = Material(
            caCertificatePem = pem(ca),
            caPrivateKey = encrypt("pki:ca-private-key", privateKeyPem(caKeys.private)),
            serverCertificatePem = pem(server),
            serverPrivateKey = encrypt("pki:server-private-key", privateKeyPem(serverKeys.private)),
            serverNames = serverNames,
        )
        writeAtomically(json.writeValueAsBytes(value))
        return value
    }

    private fun serverCertificate(
        ca: X509Certificate,
        caKey: PrivateKey,
        keys: KeyPair,
        now: Instant,
    ): X509Certificate {
        val names = GeneralNames(serverNames.distinct().map { name ->
            val type = if (name.matches(IP_ADDRESS)) GeneralName.iPAddress else GeneralName.dNSName
            GeneralName(type, name)
        }.toTypedArray())
        val subject = X500Name("CN=${serverNames.first()}")
        val builder = JcaX509v3CertificateBuilder(
            ca, serial(), Date.from(now.minus(Duration.ofMinutes(1))), Date.from(now.plus(SERVER_VALIDITY)),
            subject, keys.public,
        )
            .addExtension(Extension.basicConstraints, true, BasicConstraints(false))
            .addExtension(Extension.keyUsage, true, KeyUsage(KeyUsage.digitalSignature))
            .addExtension(Extension.extendedKeyUsage, false, ExtendedKeyUsage(KeyPurposeId.id_kp_serverAuth))
            .addExtension(Extension.subjectAlternativeName, false, names)
        return JcaX509CertificateConverter().getCertificate(
            builder.build(JcaContentSignerBuilder("SHA256withECDSA").build(caKey)),
        )
    }

    private fun keyPair(): KeyPair = KeyPairGenerator.getInstance("EC").run {
        initialize(ECGenParameterSpec("secp256r1"), RANDOM)
        generateKeyPair()
    }

    private fun encrypt(context: String, plaintext: String): StoredSecret = secrets.encrypt(context, plaintext).let {
        StoredSecret(B64.encodeToString(it.ciphertext), B64.encodeToString(it.nonce), it.keyId)
    }

    private fun decrypt(context: String, stored: StoredSecret): String = secrets.decrypt(
        context,
        EncryptedSecret(B64_DECODER.decode(stored.ciphertext), B64_DECODER.decode(stored.nonce), stored.keyId),
    )

    private fun parsePrivateKey(pem: String): PrivateKey {
        val encoded = PEMParser(StringReader(pem)).use { parser ->
            val value = parser.readObject()
            when (value) {
                is org.bouncycastle.asn1.pkcs.PrivateKeyInfo -> value.encoded
                else -> throw IllegalArgumentException("unsupported private key PEM")
            }
        }
        return KeyFactory.getInstance("EC").generatePrivate(PKCS8EncodedKeySpec(encoded))
    }

    private fun parseCertificate(pem: String): X509Certificate = CertificateFactory.getInstance("X.509")
        .generateCertificate(pem.byteInputStream()) as X509Certificate

    private fun pem(value: Any): String = StringWriter().also { output ->
        JcaPEMWriter(output).use { it.writeObject(value) }
    }.toString()

    private fun privateKeyPem(value: PrivateKey): String = StringWriter().also { output ->
        PemWriter(output).use { it.writeObject(PemObject("PRIVATE KEY", value.encoded)) }
    }.toString()

    private fun writeAtomically(bytes: ByteArray) {
        val temporary = materialFile.resolveSibling("${materialFile.fileName}.tmp")
        Files.write(temporary, bytes)
        secureFile(temporary)
        runCatching {
            Files.move(temporary, materialFile, StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
        }.getOrElse {
            Files.move(temporary, materialFile, StandardCopyOption.REPLACE_EXISTING)
        }
    }

    private data class Material(
        val caCertificatePem: String,
        val caPrivateKey: StoredSecret,
        val serverCertificatePem: String,
        val serverPrivateKey: StoredSecret,
        val serverNames: List<String>,
    )

    private data class StoredSecret(val ciphertext: String, val nonce: String, val keyId: String)

    private companion object {
        val RANDOM = SecureRandom()
        val B64: Base64.Encoder = Base64.getEncoder()
        val B64_DECODER: Base64.Decoder = Base64.getDecoder()
        val CA_VALIDITY: Duration = Duration.ofDays(3650)
        val SERVER_VALIDITY: Duration = Duration.ofDays(365)
        val NODE_VALIDITY: Duration = Duration.ofDays(30)
        val IP_ADDRESS = Regex("^[0-9a-fA-F:.]+$")

        fun serial(): BigInteger = BigInteger(160, RANDOM).abs().max(BigInteger.ONE)

        fun secureDirectory(path: Path) {
            runCatching { Files.setPosixFilePermissions(path, PosixFilePermissions.fromString("rwx------")) }
        }

        fun secureFile(path: Path) {
            runCatching { Files.setPosixFilePermissions(path, PosixFilePermissions.fromString("rw-------")) }
        }
    }
}
