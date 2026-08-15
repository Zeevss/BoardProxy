package io.boardproxy.control.subscription.infrastructure.config

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.subscription.application.IssuedSubscription
import io.boardproxy.control.subscription.application.SubscriptionLinkBuilder
import org.springframework.beans.factory.annotation.Value
import java.net.URI
import java.util.Base64

class ConfiguredSubscriptionLinkBuilder(
    override val enabled: Boolean,
    private val publicUrl: String,
    private val yandexEditorUrl: String,
    private val recoveryKeyId: String,
    private val recoveryServerPublicKey: String,
    private val json: ObjectMapper,
) : SubscriptionLinkBuilder {
    init {
        if (enabled) {
            requireHttps(publicUrl, "subscription public URL")
            val yandex = requireHttps(yandexEditorUrl, "Yandex editor URL")
            require(yandex.host in TRUSTED_YANDEX_HOSTS) { "Yandex editor URL must use disk.yandex or docs.yandex" }
            require(recoveryKeyId.isNotBlank()) { "subscription recovery key id is required" }
            requireKey(recoveryServerPublicKey, "subscription recovery server public key")
        }
    }

    override fun build(issued: IssuedSubscription): String {
        check(enabled) { "subscription links are disabled" }
        requireKey(issued.recoveryClientPrivateKey, "subscription recovery client private key")
        val capsule = mapOf(
            "version" to 1,
            "yandexUrl" to yandexEditorUrl,
            "recoveryKeyId" to recoveryKeyId,
            "clientPrivateKey" to issued.recoveryClientPrivateKey,
            "recoveryServerPublic" to recoveryServerPublicKey,
        )
        val encoded = Base64.getUrlEncoder().withoutPadding().encodeToString(json.writeValueAsBytes(capsule))
        return "${publicUrl.trimEnd('/')}/s/${issued.token}#bp1=$encoded"
    }

    private fun requireHttps(raw: String, label: String): URI {
        val uri = runCatching { URI(raw) }.getOrNull()
        require(uri != null && uri.scheme == "https" && uri.host != null) { "$label must be an absolute HTTPS URL" }
        return uri
    }

    private fun requireKey(raw: String, label: String) {
        val decoded = runCatching { Base64.getUrlDecoder().decode(raw) }.getOrNull()
        require(decoded?.size == 32 && decoded.any { it.toInt() != 0 }) { "$label must contain 32 base64url bytes" }
    }

    private companion object {
        val TRUSTED_YANDEX_HOSTS = setOf("disk.yandex.ru", "docs.yandex.ru", "disk.yandex.com", "docs.yandex.com")
    }
}
