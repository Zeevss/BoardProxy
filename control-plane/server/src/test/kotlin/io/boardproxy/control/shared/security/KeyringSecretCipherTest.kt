package io.boardproxy.control.shared.security

import java.util.Base64
import kotlin.test.Test
import kotlin.test.assertEquals

class KeyringSecretCipherTest {
    @Test
    fun `active key encrypts while old key remains readable`() {
        val oldKey = Base64.getEncoder().encodeToString(ByteArray(32) { 1 })
        val newKey = Base64.getEncoder().encodeToString(ByteArray(32) { 2 })
        val oldCipher = KeyringSecretCipher("old", mapOf("old" to oldKey))
        val encrypted = oldCipher.encrypt("context", "secret")
        val rotated = KeyringSecretCipher("new", mapOf("old" to oldKey, "new" to newKey))

        assertEquals("secret", rotated.decrypt("context", encrypted))
        assertEquals("new", rotated.encrypt("context", "next").keyId)
    }
}
