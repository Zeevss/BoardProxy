package io.boardproxy.control.shared.crypto

import java.math.BigInteger
import java.security.KeyFactory
import java.security.SecureRandom
import java.security.spec.NamedParameterSpec
import java.security.spec.XECPrivateKeySpec
import java.security.spec.XECPublicKeySpec
import javax.crypto.KeyAgreement

/**
 * Вывод публичного ключа X25519 из приватного.
 *
 * Ровно эта операция была написана в проекте трижды — в ключе пользователя, в
 * recovery-паре подписки и в самой подписке, — поэтому живёт здесь одна.
 * Обмен с базовой точкой 9 даёт публичный ключ: это и есть определение X25519.
 */
object X25519 {
    const val KEY_BYTES = 32

    fun publicKeyOf(privateKey: ByteArray): ByteArray {
        require(privateKey.size == KEY_BYTES) { "X25519 private key must contain $KEY_BYTES bytes" }
        val parameters = NamedParameterSpec.X25519
        val factory = KeyFactory.getInstance("X25519")
        val decodedPrivate = factory.generatePrivate(XECPrivateKeySpec(parameters, privateKey))
        val basePoint = factory.generatePublic(XECPublicKeySpec(parameters, BigInteger.valueOf(9)))
        return KeyAgreement.getInstance("X25519").run {
            init(decodedPrivate)
            doPhase(basePoint, true)
            generateSecret()
        }
    }

    fun generatePrivateKey(random: SecureRandom = SecureRandom()): ByteArray =
        ByteArray(KEY_BYTES).also(random::nextBytes)
}
