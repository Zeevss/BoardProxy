package ru.zevsus.proxy.boardvpn.infrastructure.persistence

import androidx.datastore.core.CorruptionException
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test
import ru.zevsus.proxy.boardvpn.domain.model.AppSettings
import ru.zevsus.proxy.boardvpn.domain.model.ThemeMode

class DataStoreAppSettingsRepositoryTest {
    @Test
    fun `settings are updated one field at a time`() = runBlocking {
        val store = FakeDataStore(AppSettings.Default)
        val repository = DataStoreAppSettingsRepository(store)

        repository.setThemeMode(ThemeMode.Light)
        repository.setDynamicColor(false)
        repository.setAutoConnectOnLaunch(true)

        assertEquals(
            AppSettings(
                themeMode = ThemeMode.Light,
                dynamicColor = false,
                autoConnectOnLaunch = true,
            ),
            repository.observeSettings().first(),
        )
    }

    @Test
    fun `settings survive a serializer round trip`() = runBlocking {
        val settings = AppSettings(themeMode = ThemeMode.Dark, autoConnectOnLaunch = true)

        val output = ByteArrayOutputStream()
        AppSettingsSerializer.writeTo(settings, output)
        val restored = AppSettingsSerializer.readFrom(ByteArrayInputStream(output.toByteArray()))

        assertEquals(settings, restored)
    }

    @Test
    fun `empty file falls back to defaults`() = runBlocking {
        val restored = AppSettingsSerializer.readFrom(ByteArrayInputStream(ByteArray(0)))

        assertEquals(AppSettings.Default, restored)
    }

    @Test(expected = CorruptionException::class)
    fun `malformed json is reported as corruption`(): Unit = runBlocking {
        AppSettingsSerializer.readFrom(ByteArrayInputStream("{oops".toByteArray()))
    }
}
