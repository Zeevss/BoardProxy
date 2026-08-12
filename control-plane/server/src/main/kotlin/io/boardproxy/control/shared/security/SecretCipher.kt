package io.boardproxy.control.shared.security

data class EncryptedSecret(
    val ciphertext: ByteArray,
    val nonce: ByteArray,
    val keyId: String,
)

interface SecretCipher {
    fun encrypt(context: String, plaintext: String): EncryptedSecret
    fun decrypt(context: String, secret: EncryptedSecret): String
}
