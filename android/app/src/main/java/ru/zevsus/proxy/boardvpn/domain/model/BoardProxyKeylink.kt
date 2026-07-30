package ru.zevsus.proxy.boardvpn.domain.model

import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import java.security.MessageDigest
import java.util.Base64

@Serializable(with = BoardProxyKeylinkSerializer::class)
class BoardProxyKeylink private constructor(
    private val rawValue: String,
    val label: String?,
) {
    fun reveal(): String = rawValue

    fun fingerprint(): String = MessageDigest.getInstance("SHA-256")
        .digest(rawValue.toByteArray(Charsets.UTF_8))
        .take(12)
        .joinToString(separator = "") { byte -> "%02x".format(byte.toInt() and 0xff) }

    override fun toString(): String = "BoardProxyKeylink(**redacted**)"

    override fun equals(other: Any?): Boolean =
        other is BoardProxyKeylink && rawValue == other.rawValue

    override fun hashCode(): Int = rawValue.hashCode()

    companion object {
        fun fromRaw(value: String): BoardProxyKeylink {
            require(value.isNotBlank()) {
                "Keylink must not be blank"
            }
            require(value == value.trim()) {
                "Keylink must be trimmed"
            }
            require(value.startsWith("bproxy://")) {
                "Unsupported keylink scheme"
            }

            var payload = value.removePrefix("bproxy://")
            val label = payload.substringAfter('#', missingDelimiterValue = "")
                .trim()
                .takeIf(String::isNotEmpty)
            payload = payload.substringBefore('#')
            val token = payload.substringBefore('@')
            require(token.matches(Regex("[A-Za-z0-9_-]+"))) {
                "Invalid keylink token"
            }
            val decoded = runCatching { Base64.getUrlDecoder().decode(token) }
                .getOrElse { throw IllegalArgumentException("Invalid keylink token", it) }
            require(decoded.size == KEY_MATERIAL_BYTES) {
                "Invalid keylink token length"
            }

            return BoardProxyKeylink(value, label)
        }

        private const val KEY_MATERIAL_BYTES = 64
    }
}

/**
 * Stores the keylink as the same raw string users import, so persisted profiles
 * stay round-trippable and are validated again by [BoardProxyKeylink.fromRaw]
 * when they are read back.
 */
object BoardProxyKeylinkSerializer : KSerializer<BoardProxyKeylink> {
    override val descriptor: SerialDescriptor =
        PrimitiveSerialDescriptor("BoardProxyKeylink", PrimitiveKind.STRING)

    override fun serialize(encoder: Encoder, value: BoardProxyKeylink) {
        encoder.encodeString(value.reveal())
    }

    override fun deserialize(decoder: Decoder): BoardProxyKeylink =
        BoardProxyKeylink.fromRaw(decoder.decodeString())
}
