package io.boardproxy.control.subscription.infrastructure.config

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.subscription.application.IssuedSubscription
import io.boardproxy.control.subscription.application.SubscriptionLinkBuilder
import io.boardproxy.control.subscription.application.SubscriptionServiceRepository
import java.util.Base64

/**
 * Ссылка собирается из настроек в базе, а не из переменных окружения:
 * оператор меняет их в панели, и правка действует без передеплоя.
 */
class StoredSubscriptionLinkBuilder(
    private val settings: SubscriptionServiceRepository,
    private val json: ObjectMapper,
) : SubscriptionLinkBuilder {

    override val enabled: Boolean
        get() = settings.settings().let { it.enabled && it.recoveryPublicKey != null }

    override fun build(issued: IssuedSubscription): String {
        val current = settings.settings()
        check(current.enabled) { "subscription links are disabled" }
        val serverPublic = requireNotNull(current.recoveryPublicKey) {
            "subscription service recovery key is not generated yet"
        }
        val capsule = mapOf(
            "version" to 1,
            "yandexUrl" to current.yandexEditorUrl,
            "recoveryKeyId" to current.recoveryKeyId,
            "clientPrivateKey" to issued.recoveryClientPrivateKey,
            "recoveryServerPublic" to serverPublic,
        )
        val encoded = Base64.getUrlEncoder().withoutPadding().encodeToString(json.writeValueAsBytes(capsule))
        return "${current.publicUrl.trimEnd('/')}/s/${issued.token}#bp1=$encoded"
    }
}
