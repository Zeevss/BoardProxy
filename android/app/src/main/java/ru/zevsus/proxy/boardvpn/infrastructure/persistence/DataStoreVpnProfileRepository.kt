package ru.zevsus.proxy.boardvpn.infrastructure.persistence

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.core.DataStoreFactory
import androidx.datastore.core.handlers.ReplaceFileCorruptionHandler
import androidx.datastore.dataStoreFile
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfile
import ru.zevsus.proxy.boardvpn.domain.model.VpnProfileId
import ru.zevsus.proxy.boardvpn.domain.repository.VpnProfileRepository

class DataStoreVpnProfileRepository internal constructor(
    private val store: DataStore<StoredProfiles>,
) : VpnProfileRepository {

    override fun observeProfiles(): Flow<List<VpnProfile>> = store.data
        .map { stored -> stored.profiles.sortedBy { it.name.lowercase() } }
        .distinctUntilChanged()

    override fun observeSelectedProfileId(): Flow<VpnProfileId?> = store.data
        .map { stored ->
            stored.selectedProfileId?.takeIf { id -> stored.profiles.any { it.id == id } }
        }
        .distinctUntilChanged()

    override suspend fun getProfile(id: VpnProfileId): VpnProfile? =
        store.data.first().profiles.firstOrNull { it.id == id }

    override suspend fun saveProfile(profile: VpnProfile) {
        store.updateData { stored ->
            val profiles = stored.profiles.filterNot { it.id == profile.id } + profile
            stored.copy(profiles = profiles)
        }
    }

    override suspend fun deleteProfile(id: VpnProfileId) {
        store.updateData { stored ->
            stored.copy(
                profiles = stored.profiles.filterNot { it.id == id },
                selectedProfileId = stored.selectedProfileId.takeIf { it != id },
            )
        }
    }

    override suspend fun selectProfile(id: VpnProfileId?) {
        store.updateData { stored -> stored.copy(selectedProfileId = id) }
    }

    companion object {
        private const val FILE_NAME = "vpn_profiles.json"

        fun create(context: Context, scope: CoroutineScope): DataStoreVpnProfileRepository =
            DataStoreVpnProfileRepository(
                DataStoreFactory.create(
                    serializer = StoredProfilesSerializer,
                    corruptionHandler = ReplaceFileCorruptionHandler {
                        StoredProfilesSerializer.defaultValue
                    },
                    scope = scope,
                    produceFile = { context.dataStoreFile(FILE_NAME) },
                )
            )
    }
}
