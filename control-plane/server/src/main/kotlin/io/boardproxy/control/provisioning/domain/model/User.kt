package io.boardproxy.control.provisioning.domain.model

import java.math.BigInteger
import java.security.KeyFactory
import java.security.spec.NamedParameterSpec
import java.security.spec.XECPrivateKeySpec
import java.security.spec.XECPublicKeySpec
import java.time.Instant
import java.util.Base64
import javax.crypto.KeyAgreement

private const val MAXIMUM_LANES = 32

data class User(
    val id: String,
    val name: String,
    val privateKey: String? = null,
    val publicKey: String? = null,
    val state: ResourceState,
    val maxSessions: Int,
    val maxLanes: Int,
    val version: Long,
    val updatedAt: Instant,
) {
    init {
        requireDomain(validId(id) && name.isNotBlank() && version > 0, "invalid user identity")
        requireDomain(maxSessions >= 0 && maxLanes in 1..MAXIMUM_LANES, "invalid user limits")
        identityPublicKey()
    }

    fun identityFingerprint(): String = Base64.getEncoder().encodeToString(identityPublicKey())

    private fun identityPublicKey(): ByteArray {
        requireDomain((privateKey == null) xor (publicKey == null), "exactly one user key is required")
        publicKey?.let { return KeyMaterial.decodePublic(it) }
        val privateBytes = KeyMaterial.decodePrivate(requireNotNull(privateKey))
        val parameters = NamedParameterSpec.X25519
        val decodedPrivate = KeyFactory.getInstance("X25519")
            .generatePrivate(XECPrivateKeySpec(parameters, privateBytes))
        val basePoint = KeyFactory.getInstance("X25519")
            .generatePublic(XECPublicKeySpec(parameters, BigInteger.valueOf(9)))
        return KeyAgreement.getInstance("X25519").run {
            init(decodedPrivate)
            doPhase(basePoint, true)
            generateSecret()
        }
    }
}

internal object KeyMaterial {
    fun decodePrivate(encoded: String): ByteArray = decode(encoded, rejectZero = false)
    fun decodePublic(encoded: String): ByteArray = decode(encoded, rejectZero = true)

    private fun decode(encoded: String, rejectZero: Boolean): ByteArray {
        requireDomain(encoded.startsWith("base64:"), "key must use base64 prefix")
        val decoded = runCatching { Base64.getDecoder().decode(encoded.removePrefix("base64:")) }
            .getOrElse { throw DomainViolation("invalid base64 key") }
        requireDomain(decoded.size == 32, "key must contain 32 bytes")
        if (rejectZero) requireDomain(decoded.any { it.toInt() != 0 }, "public key cannot be all zero")
        return decoded
    }
}
