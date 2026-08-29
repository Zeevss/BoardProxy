package io.boardproxy.control.subscription.application

import io.boardproxy.control.shared.contracts.IssuedServiceToken
import io.boardproxy.control.shared.contracts.ServiceTokenIssuer
import io.boardproxy.control.shared.audit.AuditRepository
import io.boardproxy.control.shared.audit.AuditEvent
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.subscription.domain.RecoveryKeys
import java.net.URI
import java.time.Clock
import java.time.Instant
import java.util.UUID

/**
 * Управляет настройками сервиса подписок и его наблюдаемым состоянием.
 * Сервис считается подключённым, пока он недавно приходил за конфигурацией:
 * отдельного heartbeat нет, сам запрос конфигурации им и является.
 */
class SubscriptionServiceManager(
    private val repository: SubscriptionServiceRepository,
    private val tokens: ServiceTokenIssuer,
    private val audit: AuditRepository,
    private val transactions: TransactionRunner,
    private val clock: Clock,
    private val nextId: () -> String = { UUID.randomUUID().toString() },
) : SubscriptionServiceCommands, SubscriptionServiceQueries {

    override fun settings(): SubscriptionServiceSettings = repository.settings()

    override fun status(): SubscriptionServiceStatus = repository.status()

    override fun update(
        update: SubscriptionServiceUpdate,
        expectedRevision: Long,
        actor: String,
    ): SubscriptionServiceSettings {
        if (actor.isBlank()) throw InvalidRequest("actor is required")
        val normalized = update.normalized()
        if (normalized.enabled) validateForDelivery(normalized)
        val now = clock.instant()
        return transactions.required {
            // Ключ выпускается один раз и переживает правки настроек: его смена
            // ломает все ранее выданные ссылки подписок.
            if (repository.recoveryPrivateKey() == null) {
                val pair = RecoveryKeys.generate()
                repository.saveRecoveryKeys(pair.privateKey, pair.publicKey, now)
            }
            if (!repository.replace(normalized, expectedRevision + 1, expectedRevision, now)) {
                throw ResourceConflict("subscription service settings revision changed")
            }
            audit.append(event("subscription-service.updated", actor, now, mapOf("enabled" to normalized.enabled)))
            repository.settings()
        }
    }

    override fun issueToken(actor: String): IssuedServiceToken {
        if (actor.isBlank()) throw InvalidRequest("actor is required")
        val now = clock.instant()
        return transactions.required {
            // Перевыпуск обязан обесценить прежний токен: два живых секрета у
            // одного сервиса означают, что отозвать утёкший невозможно.
            repository.tokenId()?.let { previous -> tokens.revoke(previous, actor) }
            val issued = tokens.issueSubscriberToken("subscription-service", actor)
            repository.attachToken(issued.id, now)
            audit.append(event("subscription-service.token-issued", actor, now, emptyMap()))
            issued
        }
    }

    override fun requestRestart(actor: String) {
        if (actor.isBlank()) throw InvalidRequest("actor is required")
        val now = clock.instant()
        transactions.required {
            val nonce = repository.bumpRestartNonce(now)
            audit.append(event("subscription-service.restart-requested", actor, now, mapOf("nonce" to nonce)))
        }
    }

    override fun poll(report: SubscriptionServiceReport, since: Long?): SubscriptionServiceConfig? {
        val now = clock.instant()
        repository.recordReport(report, now)
        val settings = repository.settings()
        val restartPending = repository.status().ackedRestartNonce < settings.restartNonce
        // Ревизия совпала и перезапуск не ждёт — отдавать нечего.
        if (since != null && since == settings.revision && !restartPending) return null
        val privateKey = repository.recoveryPrivateKey() ?: return null
        if (restartPending) repository.markRestartDelivered(settings.restartNonce, now)
        return SubscriptionServiceConfig(
            revision = settings.revision,
            enabled = settings.enabled,
            serviceName = settings.serviceName,
            icon = settings.icon,
            publicUrl = settings.publicUrl,
            yandexEditorUrl = settings.yandexEditorUrl,
            recoveryKeyId = settings.recoveryKeyId,
            recoveryPrivateKey = privateKey,
            apps = settings.apps,
            restartRequested = restartPending,
        )
    }

    private fun SubscriptionServiceUpdate.normalized() = copy(
        serviceName = serviceName.trim(),
        icon = icon.trim(),
        publicUrl = publicUrl.trim().trimEnd('/'),
        yandexEditorUrl = yandexEditorUrl.trim(),
        recoveryKeyId = recoveryKeyId.trim(),
        apps = apps.map { SubscriptionApp(it.platform.trim().lowercase(), it.url.trim()) }
            .filter { it.url.isNotEmpty() },
    )

    /** Включённая доставка обязана быть работоспособной, иначе ссылки собрать нельзя. */
    private fun validateForDelivery(update: SubscriptionServiceUpdate) {
        if (update.serviceName.isBlank()) throw InvalidRequest("service name is required")
        if (update.recoveryKeyId.isBlank()) throw InvalidRequest("recovery key id is required")
        requireHttps(update.publicUrl, "subscription public URL")
        val yandex = requireHttps(update.yandexEditorUrl, "Yandex editor URL")
        if (yandex.host !in TRUSTED_YANDEX_HOSTS) {
            throw InvalidRequest("Yandex editor URL must use disk.yandex or docs.yandex")
        }
        update.apps.forEach { app ->
            if (app.platform !in PLATFORMS) throw InvalidRequest("unknown client platform ${app.platform}")
            requireHttps(app.url, "client link for ${app.platform}")
        }
    }

    private fun requireHttps(raw: String, label: String): URI {
        val uri = runCatching { URI(raw) }.getOrNull()
        if (uri == null || uri.scheme != "https" || uri.host == null) {
            throw InvalidRequest("$label must be an absolute HTTPS URL")
        }
        return uri
    }

    private fun event(action: String, actor: String, now: Instant, details: Map<String, Any>) = AuditEvent(
        id = nextId(), nodeId = null, actor = actor, action = action,
        resourceType = "subscription-service", resourceId = "singleton", resourceVersion = 0,
        details = details, occurredAt = now,
    )

    private companion object {
        val TRUSTED_YANDEX_HOSTS = setOf("disk.yandex.ru", "docs.yandex.ru", "disk.yandex.com", "docs.yandex.com")
        val PLATFORMS = setOf("ios", "android", "windows", "macos", "linux")
    }
}
