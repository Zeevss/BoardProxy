package ru.zevsus.proxy.boardvpn.infrastructure.persistence

import androidx.datastore.core.CorruptionException
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test
import ru.zevsus.proxy.boardvpn.domain.model.BoardProxyKeylink
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId

class StoredProfilesSerializerTest {
    private val profile = VpnProfile(
        id = VpnProfileId("home"),
        name = "Home",
        keylink = BoardProxyKeylink.fromRaw("bproxy://${"A".repeat(86)}"),
    )

    @Test
    fun `snapshot survives a write read round trip`() = runBlocking {
        val snapshot = StoredProfiles(
            profiles = listOf(profile),
            selectedProfileId = profile.id,
        )

        assertEquals(snapshot, roundTrip(snapshot))
    }

    @Test
    fun `empty file falls back to the default value`() = runBlocking {
        val restored = StoredProfilesSerializer.readFrom(ByteArrayInputStream(ByteArray(0)))

        assertEquals(StoredProfilesSerializer.defaultValue, restored)
    }

    @Test(expected = CorruptionException::class)
    fun `malformed json is reported as corruption`(): Unit = runBlocking {
        StoredProfilesSerializer.readFrom(ByteArrayInputStream("{not json".toByteArray()))
    }

    @Test(expected = CorruptionException::class)
    fun `invalid persisted profile is reported as corruption`(): Unit = runBlocking {
        val payload = """{"profiles":[{"id":"home","name":"Home","keylink":"broken"}]}"""

        StoredProfilesSerializer.readFrom(ByteArrayInputStream(payload.toByteArray()))
    }

    private suspend fun roundTrip(snapshot: StoredProfiles): StoredProfiles {
        val output = ByteArrayOutputStream()
        StoredProfilesSerializer.writeTo(snapshot, output)
        return StoredProfilesSerializer.readFrom(ByteArrayInputStream(output.toByteArray()))
    }
}
