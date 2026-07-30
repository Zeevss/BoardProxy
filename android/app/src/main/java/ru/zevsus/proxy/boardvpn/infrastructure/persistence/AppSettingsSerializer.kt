package ru.zevsus.proxy.boardvpn.infrastructure.persistence

import androidx.datastore.core.CorruptionException
import androidx.datastore.core.Serializer
import java.io.InputStream
import java.io.OutputStream
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import ru.zevsus.proxy.boardvpn.domain.model.AppSettings

internal object AppSettingsSerializer : Serializer<AppSettings> {
    private val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
    }

    override val defaultValue: AppSettings = AppSettings.Default

    override suspend fun readFrom(input: InputStream): AppSettings {
        val text = input.readBytes().decodeToString()
        if (text.isBlank()) return defaultValue

        return try {
            json.decodeFromString(AppSettings.serializer(), text)
        } catch (error: SerializationException) {
            throw CorruptionException("Stored settings are not readable", error)
        } catch (error: IllegalArgumentException) {
            throw CorruptionException("Stored settings are not valid", error)
        }
    }

    override suspend fun writeTo(t: AppSettings, output: OutputStream) {
        output.write(json.encodeToString(AppSettings.serializer(), t).encodeToByteArray())
    }
}
