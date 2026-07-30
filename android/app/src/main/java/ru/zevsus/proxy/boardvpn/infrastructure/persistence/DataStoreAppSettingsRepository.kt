package ru.zevsus.proxy.boardvpn.infrastructure.persistence

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.core.DataStoreFactory
import androidx.datastore.core.handlers.ReplaceFileCorruptionHandler
import androidx.datastore.dataStoreFile
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import ru.zevsus.proxy.boardvpn.domain.model.AppRoutingPolicy
import ru.zevsus.proxy.boardvpn.domain.model.AppSettings
import ru.zevsus.proxy.boardvpn.domain.model.ThemeMode
import ru.zevsus.proxy.boardvpn.domain.repository.AppSettingsRepository

/** User preferences stored next to the profiles, in their own JSON file. */
class DataStoreAppSettingsRepository internal constructor(
    private val store: DataStore<AppSettings>,
) : AppSettingsRepository {
    override fun observeSettings(): Flow<AppSettings> = store.data.distinctUntilChanged()

    override suspend fun setThemeMode(mode: ThemeMode) {
        store.updateData { it.copy(themeMode = mode) }
    }

    override suspend fun setAutoConnectOnLaunch(enabled: Boolean) {
        store.updateData { it.copy(autoConnectOnLaunch = enabled) }
    }

    override suspend fun setAppRoutingPolicy(policy: AppRoutingPolicy) {
        store.updateData { it.copy(appRoutingPolicy = policy) }
    }

    companion object {
        private const val FILE_NAME = "app_settings.json"

        fun create(context: Context, scope: CoroutineScope): DataStoreAppSettingsRepository =
            DataStoreAppSettingsRepository(
                DataStoreFactory.create(
                    serializer = AppSettingsSerializer,
                    corruptionHandler = ReplaceFileCorruptionHandler {
                        AppSettingsSerializer.defaultValue
                    },
                    scope = scope,
                    produceFile = { context.dataStoreFile(FILE_NAME) },
                )
            )
    }
}
