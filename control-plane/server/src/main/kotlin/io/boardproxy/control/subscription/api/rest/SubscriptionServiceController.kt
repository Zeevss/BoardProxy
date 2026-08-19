package io.boardproxy.control.subscription.api.rest

import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.subscription.application.SubscriptionApp
import io.boardproxy.control.subscription.application.SubscriptionServiceCommands
import io.boardproxy.control.subscription.application.SubscriptionServiceConfig
import io.boardproxy.control.subscription.application.SubscriptionServiceQueries
import io.boardproxy.control.subscription.application.SubscriptionServiceReport
import io.boardproxy.control.subscription.application.SubscriptionServiceSettings
import io.boardproxy.control.subscription.application.SubscriptionServiceStatus
import io.boardproxy.control.subscription.application.SubscriptionServiceUpdate
import org.springframework.http.ResponseEntity
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.PutMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestHeader
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController
import java.security.Principal
import java.time.Clock
import java.time.Duration
import java.time.Instant

/**
 * Панельная часть управления сервисом подписок. Сервисный канал живёт
 * отдельным контроллером: у него другая роль и другой контракт.
 */
@RestController
@RequestMapping("/api/v1/subscription-service")
class SubscriptionServiceController(
    private val commands: SubscriptionServiceCommands,
    private val queries: SubscriptionServiceQueries,
    private val clock: Clock,
) {
    @GetMapping
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun get(): SubscriptionServiceResponse = response()

    @PutMapping
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun update(
        @RequestHeader("If-Match") ifMatch: String,
        @RequestBody request: UpdateSubscriptionServiceRequest,
        principal: Principal,
    ): ResponseEntity<SubscriptionServiceResponse> {
        val revision = ifMatch.removeSurrounding("\"").toLongOrNull()
            ?: throw InvalidRequest("If-Match must contain the numeric settings revision")
        val updated = commands.update(request.toUpdate(), revision, principal.name)
        return ResponseEntity.ok().eTag(updated.revision.toString()).body(response(updated))
    }

    @PostMapping("/token")
    @PreAuthorize("hasRole('ADMIN')")
    fun issueToken(principal: Principal): IssuedServiceTokenResponse {
        val issued = commands.issueToken(principal.name)
        return IssuedServiceTokenResponse(issued.id, "subscription-service", issued.secret)
    }

    @PostMapping("/restart")
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun restart(principal: Principal): ResponseEntity<Void> {
        commands.requestRestart(principal.name)
        return ResponseEntity.accepted().build()
    }

    private fun response(settings: SubscriptionServiceSettings = queries.settings()): SubscriptionServiceResponse {
        val status = queries.status()
        return SubscriptionServiceResponse(settings.toResponse(), status.toResponse(settings, clock.instant()))
    }
}

data class UpdateSubscriptionServiceRequest(
    val enabled: Boolean = false,
    val serviceName: String = "",
    val icon: String = "",
    val publicUrl: String = "",
    val yandexEditorUrl: String = "",
    val recoveryKeyId: String = "",
    val apps: List<SubscriptionAppRequest> = emptyList(),
)

data class SubscriptionAppRequest(val platform: String, val url: String)

data class SubscriptionServiceResponse(
    val settings: SubscriptionServiceSettingsResponse,
    val status: SubscriptionServiceStatusResponse,
)

data class SubscriptionServiceSettingsResponse(
    val enabled: Boolean,
    val serviceName: String,
    val icon: String,
    val publicUrl: String,
    val yandexEditorUrl: String,
    val recoveryKeyId: String,
    val apps: List<SubscriptionAppRequest>,
    val revision: Long,
    val updatedAt: Instant,
)

data class SubscriptionServiceStatusResponse(
    val tokenIssued: Boolean,
    val connected: Boolean,
    val lastSeenAt: Instant?,
    val serviceVersion: String?,
    val appliedRevision: Long?,
    val recoveryWatcherReady: Boolean?,
    val recoveryPublicKey: String?,
    val startedAt: Instant?,
)

data class IssuedServiceTokenResponse(val id: String, val name: String, val secret: String)

private fun UpdateSubscriptionServiceRequest.toUpdate() = SubscriptionServiceUpdate(
    enabled = enabled, serviceName = serviceName, icon = icon, publicUrl = publicUrl,
    yandexEditorUrl = yandexEditorUrl, recoveryKeyId = recoveryKeyId,
    apps = apps.map { SubscriptionApp(it.platform, it.url) },
)

private fun SubscriptionServiceSettings.toResponse() = SubscriptionServiceSettingsResponse(
    enabled = enabled, serviceName = serviceName, icon = icon, publicUrl = publicUrl,
    yandexEditorUrl = yandexEditorUrl, recoveryKeyId = recoveryKeyId,
    apps = apps.map { SubscriptionAppRequest(it.platform, it.url) },
    revision = revision, updatedAt = updatedAt,
)

/**
 * Подключённость выводится из давности последнего опроса: отдельного heartbeat
 * нет, поэтому «подключен» означает «приходил за конфигурацией недавно».
 */
private fun SubscriptionServiceStatus.toResponse(settings: SubscriptionServiceSettings, now: Instant) =
    SubscriptionServiceStatusResponse(
        tokenIssued = settings.tokenIssued,
        connected = lastSeenAt?.isAfter(now.minus(OFFLINE_AFTER)) == true,
        lastSeenAt = lastSeenAt,
        serviceVersion = serviceVersion,
        appliedRevision = appliedRevision,
        recoveryWatcherReady = recoveryWatcherReady,
        recoveryPublicKey = settings.recoveryPublicKey,
        startedAt = startedAt,
    )

/** Три пропущенных опроса подряд: одиночный сетевой сбой не должен гасить индикатор. */
private val OFFLINE_AFTER: Duration = Duration.ofSeconds(45)
