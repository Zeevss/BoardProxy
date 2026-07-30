package ru.zevsus.proxy.boardvpn.infrastructure.persistence

import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import ru.zevsus.proxy.boardvpn.domain.model.BoardProxyKeylink
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId

class DataStoreVpnProfileRepositoryTest {
    private val home = profile("home", "Home")
    private val work = profile("work", "work")

    @Test
    fun `profiles are saved, observed alphabetically and deleted`() = runBlocking {
        val repository = repository()

        repository.saveProfile(work)
        repository.saveProfile(home)

        assertEquals(home, repository.getProfile(home.id))
        assertEquals(listOf(home, work), repository.observeProfiles().first())

        repository.deleteProfile(home.id)
        assertNull(repository.getProfile(home.id))
        assertEquals(listOf(work), repository.observeProfiles().first())
    }

    @Test
    fun `saving the same id replaces the stored profile`() = runBlocking {
        val repository = repository()
        val renamed = home.copy(name = "Home renamed")

        repository.saveProfile(home)
        repository.saveProfile(renamed)

        assertEquals(listOf(renamed), repository.observeProfiles().first())
    }

    @Test
    fun `selection is persisted and cleared together with its profile`() = runBlocking {
        val repository = repository()
        repository.saveProfile(home)

        repository.selectProfile(home.id)
        assertEquals(home.id, repository.observeSelectedProfileId().first())

        repository.deleteProfile(home.id)
        assertNull(repository.observeSelectedProfileId().first())
    }

    @Test
    fun `selection pointing at a missing profile is not exposed`() = runBlocking {
        val repository = repository(
            StoredProfiles(profiles = listOf(home), selectedProfileId = work.id)
        )

        assertNull(repository.observeSelectedProfileId().first())
    }

    private fun repository(initial: StoredProfiles = StoredProfiles()) =
        DataStoreVpnProfileRepository(FakeDataStore(initial))

    private fun profile(id: String, name: String) = VpnProfile(
        id = VpnProfileId(id),
        name = name,
        keylink = BoardProxyKeylink.fromRaw("bproxy://${"A".repeat(86)}#$id"),
    )
}
