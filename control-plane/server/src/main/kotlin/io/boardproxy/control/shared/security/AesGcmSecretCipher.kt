package io.boardproxy.control.shared.security

import java.security.SecureRandom
import java.util.Base64
import javax.crypto.Cipher
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

class AesGcmSecretCipher(
    encodedMasterKey: String,
    private val keyId: String,
    private val secureRandom: SecureRandom = SecureRandom(),
) : SecretCipher {
    private val key = run {
        val raw = runCatching { Base64.getDecoder().decode(encodedMasterKey) }
            .getOrElse { throw IllegalArgumentException("CONTROL_MASTER_KEY must be valid base64", it) }
        require(raw.size == 32) { "CONTROL_MASTER_KEY must contain exactly 32 bytes" }
        require(keyId.isNotBlank()) { "CONTROL_MASTER_KEY_ID is required" }
        SecretKeySpec(raw, "AES")
    }

    override fun encrypt(context: String, plaintext: String): EncryptedSecret {
        require(context.isNotBlank()) { "secret context is required" }
        val nonce = ByteArray(NONCE_BYTES).also(secureRandom::nextBytes)
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, key, GCMParameterSpec(TAG_BITS, nonce))
        cipher.updateAAD(context.toByteArray(Charsets.UTF_8))
        return EncryptedSecret(cipher.doFinal(plaintext.toByteArray(Charsets.UTF_8)), nonce, keyId)
    }

    override fun decrypt(context: String, secret: EncryptedSecret): String {
        require(secret.keyId == keyId) { "encrypted secret references unavailable key ${secret.keyId}" }
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, key, GCMParameterSpec(TAG_BITS, secret.nonce))
        cipher.updateAAD(context.toByteArray(Charsets.UTF_8))
        return cipher.doFinal(secret.ciphertext).toString(Charsets.UTF_8)
    }

    private companion object {
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val NONCE_BYTES = 12
        const val TAG_BITS = 128
    }
}
