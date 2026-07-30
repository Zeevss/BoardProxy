package ru.zevsus.proxy.boardvpn.domain.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class BoardProxyKeylinkTest {
    @Test
    fun `raw value is never exposed by toString`() {
        val raw = "bproxy://${"A".repeat(86)}#test"
        val keylink = BoardProxyKeylink.fromRaw(raw)

        assertEquals(raw, keylink.reveal())
        assertEquals("BoardProxyKeylink(**redacted**)", keylink.toString())
        assertTrue(raw !in keylink.toString())
    }

    @Test
    fun `blank keylink is rejected`() {
        assertThrows(IllegalArgumentException::class.java) {
            BoardProxyKeylink.fromRaw(" ")
        }
    }

    @Test
    fun `foreign scheme is rejected`() {
        assertThrows(IllegalArgumentException::class.java) {
            BoardProxyKeylink.fromRaw("https://example.com")
        }
    }

    @Test
    fun `invalid token length is rejected`() {
        assertThrows(IllegalArgumentException::class.java) {
            BoardProxyKeylink.fromRaw("bproxy://short")
        }
    }

    @Test
    fun `label and stable fingerprint are extracted`() {
        val raw = "bproxy://${"A".repeat(86)}@board#test-android"

        val keylink = BoardProxyKeylink.fromRaw(raw)

        assertEquals("test-android", keylink.label)
        assertEquals(keylink.fingerprint(), BoardProxyKeylink.fromRaw(raw).fingerprint())
    }
}
