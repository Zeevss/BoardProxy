package ru.zevsus.proxy.boardvpn.domain.model

import java.security.MessageDigest
import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder

@Serializable(with = BoardProxySubscriptionUrlSerializer::class)
class BoardProxySubscriptionUrl private constructor(private val rawValue: String) {
    fun reveal(): String = rawValue

    fun fingerprint(): String = MessageDigest.getInstance("SHA-256")
        .digest(rawValue.toByteArray(Charsets.UTF_8))
        .take(12)
        .joinToString("") { byte -> "%02x".format(byte.toInt() and 0xff) }

    override fun toString(): String = "BoardProxySubscriptionUrl(**redacted**)"

    override fun equals(other: Any?): Boolean =
        other is BoardProxySubscriptionUrl && rawValue == other.rawValue

    override fun hashCode(): Int = rawValue.hashCode()

    companion object {
        fun fromRaw(value: String): BoardProxySubscriptionUrl {
            require(value == value.trim() && value.isNotEmpty()) { "Subscription URL must be trimmed" }
            val scheme = value.substringBefore(":").lowercase()
            require(scheme == "https" || scheme == "http") { "Unsupported subscription URL scheme" }
            require(value.substringBefore('#').contains("/s/")) { "Invalid subscription URL path" }
            require(value.substringAfter('#', "").startsWith("bp1=")) { "Recovery capsule is missing" }
            return BoardProxySubscriptionUrl(value)
        }
    }
}

object BoardProxySubscriptionUrlSerializer : KSerializer<BoardProxySubscriptionUrl> {
    override val descriptor: SerialDescriptor =
        PrimitiveSerialDescriptor("BoardProxySubscriptionUrl", PrimitiveKind.STRING)

    override fun serialize(encoder: Encoder, value: BoardProxySubscriptionUrl) {
        encoder.encodeString(value.reveal())
    }

    override fun deserialize(decoder: Decoder): BoardProxySubscriptionUrl =
        BoardProxySubscriptionUrl.fromRaw(decoder.decodeString())
}

@Serializable
data class SubscriptionKeySummary(
    val id: String,
    val name: String,
    val nodeId: String,
    val state: String,
    val usedBytes: Long,
)

@Serializable
data class VpnSubscription(
    val url: BoardProxySubscriptionUrl,
    val id: String,
    val revision: String,
    val keys: List<SubscriptionKeySummary>,
    val selectedKeyId: String = "",
    val updatedAtEpochMillis: Long = 0L,
)
