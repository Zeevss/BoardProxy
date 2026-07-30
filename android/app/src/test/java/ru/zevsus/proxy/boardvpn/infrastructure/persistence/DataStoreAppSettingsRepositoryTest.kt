package ru.zevsus.proxy.boardvpn.infrastructure.persistence

import androidx.datastore.core.CorruptionException
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import ru.zevsus.proxy.boardvpn.domain.model.AppRoutingMode
import ru.zevsus.proxy.boardvpn.domain.model.AppRoutingPolicy
import ru.zevsus.proxy.boardvpn.domain.model.AppSettings
import ru.zevsus.proxy.boardvpn.domain.model.ThemeMode

class DataStoreAppSettingsRepositoryTest {
    @Test
    fun `settings are updated one field at a time`() = runBlocking {
        val store = FakeDataStore(AppSettings.Default)
        val repository = DataStoreAppSettingsRepository(store)

        repository.setThemeMode(ThemeMode.Light)
        repository.setAutoConnectOnLaunch(true)
        repository.setAppRoutingPolicy(
            AppRoutingPolicy(
                mode = AppRoutingMode.ExcludeSelectedApps,
                packageNames = setOf("com.example.direct"),
            )
        )

        assertEquals(
            AppSettings(
                themeMode = ThemeMode.Light,
                autoConnectOnLaunch = true,
                appRoutingPolicy = AppRoutingPolicy(
                    mode = AppRoutingMode.ExcludeSelectedApps,
                    packageNames = setOf("com.example.direct"),
                ),
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

    @Test
    fun `settings written before app routing support use all apps`() = runBlocking {
        val legacyJson = """
            {"themeMode":"Dark","dynamicColor":false,"autoConnectOnLaunch":true}
        """.trimIndent()

        val restored = AppSettingsSerializer.readFrom(
            ByteArrayInputStream(legacyJson.toByteArray())
        )

        assertEquals(AppRoutingPolicy.AllApps, restored.appRoutingPolicy)
    }

    @Test
    fun `legacy selected-app policy remains active without all proxy field`() = runBlocking {
        val legacyJson = """
            {
              "appRoutingPolicy": {
                "mode": "OnlySelectedApps",
                "packageNames": ["com.example.video"]
              }
            }
        """.trimIndent()

        val restored = AppSettingsSerializer.readFrom(
            ByteArrayInputStream(legacyJson.toByteArray())
        )

        assertFalse(restored.appRoutingPolicy.allProxy)
        assertEquals(
            setOf("com.example.video"),
            restored.appRoutingPolicy.packageNames,
        )
    }

    @Test
    fun `legacy all-app policy enables all proxy`() = runBlocking {
        val legacyJson = """
            {
              "appRoutingPolicy": {
                "mode": "AllApps",
                "packageNames": []
              }
            }
        """.trimIndent()

        val restored = AppSettingsSerializer.readFrom(
            ByteArrayInputStream(legacyJson.toByteArray())
        )

        assertTrue(restored.appRoutingPolicy.allProxy)
    }

    @Test(expected = CorruptionException::class)
    fun `malformed json is reported as corruption`(): Unit = runBlocking {
        AppSettingsSerializer.readFrom(ByteArrayInputStream("{oops".toByteArray()))
    }
}
