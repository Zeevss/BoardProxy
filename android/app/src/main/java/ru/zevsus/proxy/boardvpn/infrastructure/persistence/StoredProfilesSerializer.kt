package ru.zevsus.proxy.boardvpn.infrastructure.persistence

import androidx.datastore.core.CorruptionException
import androidx.datastore.core.Serializer
import java.io.InputStream
import java.io.OutputStream
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json

internal object StoredProfilesSerializer : Serializer<StoredProfiles> {
    private val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
    }

    override val defaultValue: StoredProfiles = StoredProfiles()

    override suspend fun readFrom(input: InputStream): StoredProfiles {
        val text = input.readBytes().decodeToString()
        if (text.isBlank()) return defaultValue

        return try {
            json.decodeFromString(StoredProfiles.serializer(), text)
        } catch (error: SerializationException) {
            throw CorruptionException("Stored profiles are not readable", error)
        } catch (error: IllegalArgumentException) {
            throw CorruptionException("Stored profiles are not valid", error)
        }
    }

    override suspend fun writeTo(t: StoredProfiles, output: OutputStream) {
        output.write(json.encodeToString(StoredProfiles.serializer(), t).encodeToByteArray())
    }
}
