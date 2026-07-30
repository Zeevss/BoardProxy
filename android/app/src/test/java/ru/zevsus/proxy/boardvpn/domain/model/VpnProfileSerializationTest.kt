package ru.zevsus.proxy.boardvpn.domain.model

import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class VpnProfileSerializationTest {
    private val json = Json

    @Test
    fun `profile survives a json round trip`() {
        val raw = "bproxy://${"A".repeat(86)}#Home"
        val profile = VpnProfile.fromKeylink(BoardProxyKeylink.fromRaw(raw))

        val encoded = json.encodeToString(VpnProfile.serializer(), profile)
        val decoded = json.decodeFromString(VpnProfile.serializer(), encoded)

        assertEquals(profile, decoded)
        assertEquals(raw, decoded.keylink.reveal())
    }

    @Test
    fun `keylink is encoded as a plain string`() {
        val raw = "bproxy://${"A".repeat(86)}"
        val encoded = json.encodeToString(
            VpnProfile.serializer(),
            VpnProfile(VpnProfileId("home"), "Home", BoardProxyKeylink.fromRaw(raw)),
        )

        assertEquals("""{"id":"home","name":"Home","keylink":"$raw"}""", encoded)
    }

    @Test
    fun `invalid persisted keylink is rejected on decode`() {
        val encoded = """{"id":"home","name":"Home","keylink":"not-a-key"}"""

        val error = runCatching {
            json.decodeFromString(VpnProfile.serializer(), encoded)
        }.exceptionOrNull()

        assertTrue(
            "Unexpected error: $error",
            error is IllegalArgumentException || error is SerializationException,
        )
    }
}
