package io.boardproxy.control.subscription.domain

import java.math.BigInteger
import java.security.KeyFactory
import java.security.SecureRandom
import java.security.spec.NamedParameterSpec
import java.security.spec.XECPrivateKeySpec
import java.security.spec.XECPublicKeySpec
import java.util.Base64
import javax.crypto.KeyAgreement

data class RecoveryKeyPair(val privateKey: String, val publicKey: String)

/** X25519-пара резервного канала: одна для сервиса, отдельные — для подписок. */
object RecoveryKeys {
    fun generate(random: SecureRandom = SecureRandom()): RecoveryKeyPair {
        val privateKey = ByteArray(32).also(random::nextBytes)
        return RecoveryKeyPair(privateKey.base64Url(), publicKeyOf(privateKey).base64Url())
    }

    fun publicKeyOf(privateKey: ByteArray): ByteArray {
        val parameters = NamedParameterSpec.X25519
        val decodedPrivate = KeyFactory.getInstance("X25519")
            .generatePrivate(XECPrivateKeySpec(parameters, privateKey))
        val basePoint = KeyFactory.getInstance("X25519")
            .generatePublic(XECPublicKeySpec(parameters, BigInteger.valueOf(9)))
        return KeyAgreement.getInstance("X25519").run {
            init(decodedPrivate)
            doPhase(basePoint, true)
            generateSecret()
        }
    }

    fun decode(encoded: String): ByteArray = Base64.getUrlDecoder().decode(encoded)
}

internal fun ByteArray.base64Url(): String = Base64.getUrlEncoder().withoutPadding().encodeToString(this)
