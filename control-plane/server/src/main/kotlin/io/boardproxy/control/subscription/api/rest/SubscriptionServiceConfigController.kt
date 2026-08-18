package io.boardproxy.control.subscription.api.rest

import io.boardproxy.control.subscription.application.SubscriptionApp
import io.boardproxy.control.subscription.application.SubscriptionServiceConfig
import io.boardproxy.control.subscription.application.SubscriptionServiceQueries
import io.boardproxy.control.subscription.application.SubscriptionServiceReport
import org.springframework.http.ResponseEntity
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController
import java.time.Instant

/**
 * Единственный канал сервиса подписок. Один запрос делает три вещи сразу:
 * сообщает состояние сервиса, забирает свежую конфигурацию и узнаёт о
 * запрошенном перезапуске. Отдельного heartbeat поэтому не нужно.
 *
 * 204 означает «у тебя уже актуально»: перекачивать секреты каждый раз незачем.
 */
@RestController
@RequestMapping("/api/v1/subscription-service/poll")
class SubscriptionServiceConfigController(private val queries: SubscriptionServiceQueries) {
    @PostMapping
    @PreAuthorize("hasAnyRole('SUBSCRIBER', 'ADMIN')")
    fun poll(@RequestBody request: SubscriptionServicePollRequest): ResponseEntity<SubscriptionServiceConfigResponse> {
        val config = queries.poll(request.toReport(), request.revision)
            ?: return ResponseEntity.noContent().build()
        return ResponseEntity.ok(config.toResponse())
    }
}

data class SubscriptionServicePollRequest(
    /** Ревизия, которую сервис уже применил; null на первом запросе после старта. */
    val revision: Long? = null,
    val serviceVersion: String? = null,
    val recoveryWatcherReady: Boolean? = null,
    val startedAt: Instant? = null,
)

data class SubscriptionServiceConfigResponse(
    val revision: Long,
    val enabled: Boolean,
    val serviceName: String,
    val icon: String,
    val publicUrl: String,
    val yandexEditorUrl: String,
    val recoveryKeyId: String,
    val recoveryPrivateKey: String,
    val apps: List<SubscriptionAppRequest>,
    /** true — оператор запросил перезапуск; сервису следует завершиться. */
    val restartRequested: Boolean,
)

private fun SubscriptionServicePollRequest.toReport() = SubscriptionServiceReport(
    serviceVersion = serviceVersion,
    appliedRevision = revision,
    recoveryWatcherReady = recoveryWatcherReady,
    startedAt = startedAt,
)

private fun SubscriptionServiceConfig.toResponse() = SubscriptionServiceConfigResponse(
    revision = revision, enabled = enabled, serviceName = serviceName, icon = icon,
    publicUrl = publicUrl, yandexEditorUrl = yandexEditorUrl, recoveryKeyId = recoveryKeyId,
    recoveryPrivateKey = recoveryPrivateKey,
    apps = apps.map { app: SubscriptionApp -> SubscriptionAppRequest(app.platform, app.url) },
    restartRequested = restartRequested,
)
