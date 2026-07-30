package ru.zevsus.proxy.boardvpn.infrastructure.fake

import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import ru.zevsus.proxy.boardvpn.domain.model.BoardProxyKeylink
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId

class InMemoryVpnProfileRepositoryTest {
    @Test
    fun `profiles can be observed saved and deleted`() = runBlocking {
        val repository = InMemoryVpnProfileRepository()
        val profile = VpnProfile(
            id = VpnProfileId("home"),
            name = "Home",
            keylink = BoardProxyKeylink.fromRaw("bproxy://${"A".repeat(86)}"),
        )

        repository.saveProfile(profile)
        assertEquals(profile, repository.getProfile(profile.id))
        assertEquals(listOf(profile), repository.observeProfiles().first())

        repository.deleteProfile(profile.id)
        assertNull(repository.getProfile(profile.id))
    }

    @Test
    fun `selection is cleared together with its profile`() = runBlocking {
        val profile = VpnProfile(
            id = VpnProfileId("home"),
            name = "Home",
            keylink = BoardProxyKeylink.fromRaw("bproxy://${"A".repeat(86)}"),
        )
        val repository = InMemoryVpnProfileRepository(listOf(profile))

        repository.selectProfile(profile.id)
        assertEquals(profile.id, repository.observeSelectedProfileId().first())

        repository.deleteProfile(profile.id)
        assertNull(repository.observeSelectedProfileId().first())
    }
}
