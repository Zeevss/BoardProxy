package io.boardproxy.control.shared.security

import java.util.Base64
import javax.crypto.AEADBadTagException
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse

class AesGcmSecretCipherTest {
    private val masterKey = Base64.getEncoder().encodeToString(ByteArray(32) { 7 })

    @Test
    fun `secret round trips without storing plaintext`() {
        val cipher = AesGcmSecretCipher(masterKey, "key-v1")

        val encrypted = cipher.encrypt("node:node-1:server-key", "base64:private")

        assertFalse(encrypted.ciphertext.contentEquals("base64:private".toByteArray()))
        assertEquals("base64:private", cipher.decrypt("node:node-1:server-key", encrypted))
        assertEquals("key-v1", encrypted.keyId)
        assertEquals(12, encrypted.nonce.size)
    }

    @Test
    fun `ciphertext cannot be moved to another resource context`() {
        val cipher = AesGcmSecretCipher(masterKey, "key-v1")
        val encrypted = cipher.encrypt("user:alice:private-key", "secret")

        assertFailsWith<AEADBadTagException> {
            cipher.decrypt("user:bob:private-key", encrypted)
        }
    }
}
