package io.boardproxy.control.provisioning.domain.model

import io.boardproxy.control.shared.crypto.X25519
import io.boardproxy.control.shared.crypto.base64
import java.time.Instant
import java.util.Base64

private const val MAXIMUM_LANES = 32

/**
 * Пользователь флотовый: один человек — одна строка и один ключ независимо от
 * того, на скольких нодах он размещён. Ноды пользователь не знает; где он
 * доступен, описывают [Grant].
 */
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

    /** Уникален во всём флоте: копий пользователя по нодам больше нет. */
    fun identityFingerprint(): String = identityPublicKey().base64()

    private fun identityPublicKey(): ByteArray {
        requireDomain((privateKey == null) xor (publicKey == null), "exactly one user key is required")
        publicKey?.let { return KeyMaterial.decodePublic(it) }
        return X25519.publicKeyOf(KeyMaterial.decodePrivate(requireNotNull(privateKey)))
    }
}

internal object KeyMaterial {
    fun decodePrivate(encoded: String): ByteArray = decode(encoded, rejectZero = false)
    fun decodePublic(encoded: String): ByteArray = decode(encoded, rejectZero = true)

    private fun decode(encoded: String, rejectZero: Boolean): ByteArray {
        requireDomain(encoded.startsWith("base64:"), "key must use base64 prefix")
        val decoded = runCatching { Base64.getDecoder().decode(encoded.removePrefix("base64:")) }
            .getOrElse { throw DomainViolation("invalid base64 key") }
        requireDomain(decoded.size == X25519.KEY_BYTES, "key must contain ${X25519.KEY_BYTES} bytes")
        if (rejectZero) requireDomain(decoded.any { it.toInt() != 0 }, "public key cannot be all zero")
        return decoded
    }
}
