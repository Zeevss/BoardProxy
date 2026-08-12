package io.boardproxy.control.shared.security

class KeyringSecretCipher(
    activeKeyId: String,
    encodedKeys: Map<String, String>,
) : SecretCipher {
    private val active = activeKeyId
    private val ciphers = encodedKeys.mapValues { (id, key) -> AesGcmSecretCipher(key, id) }

    init {
        require(active.isNotBlank() && active in ciphers) { "active master key must exist in the keyring" }
    }

    override fun encrypt(context: String, plaintext: String): EncryptedSecret =
        requireNotNull(ciphers[active]).encrypt(context, plaintext)

    override fun decrypt(context: String, secret: EncryptedSecret): String =
        (ciphers[secret.keyId] ?: error("encrypted secret references unavailable key ${secret.keyId}"))
            .decrypt(context, secret)
}
