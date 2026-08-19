package io.boardproxy.control.subscription.application

import io.boardproxy.control.shared.contracts.IssuedServiceToken
import java.time.Instant

data class SubscriptionApp(val platform: String, val url: String)

/**
 * Настройки сервиса подписок. Владелец — control-plane; сам subscribe хранит
 * только адрес хаба и свой токен, а всё остальное забирает по этому каналу.
 */
data class SubscriptionServiceSettings(
    val enabled: Boolean,
    val serviceName: String,
    val icon: String,
    val publicUrl: String,
    val yandexEditorUrl: String,
    val recoveryKeyId: String,
    val recoveryPublicKey: String?,
    val apps: List<SubscriptionApp>,
    val revision: Long,
    val restartNonce: Long,
    val tokenIssued: Boolean,
    val updatedAt: Instant,
)

/** Что оператор может изменить; recovery-пару и ревизию ведёт сам control-plane. */
data class SubscriptionServiceUpdate(
    val enabled: Boolean,
    val serviceName: String,
    val icon: String,
    val publicUrl: String,
    val yandexEditorUrl: String,
    val recoveryKeyId: String,
    val apps: List<SubscriptionApp>,
)

/** Наблюдаемое состояние: заполняется, когда сервис приходит за конфигурацией. */
data class SubscriptionServiceStatus(
    val lastSeenAt: Instant?,
    val serviceVersion: String?,
    val appliedRevision: Long?,
    val recoveryWatcherReady: Boolean?,
    val startedAt: Instant?,
    val ackedRestartNonce: Long,
)

/**
 * Отчёт сервиса. Подтверждение перезапуска сюда не входит намеренно: сервис
 * не хранит состояние между запусками и после рестарта отчитался бы нулём,
 * получив команду перезапуска снова — и так бесконечно. Факт доставки
 * перезапуска ведёт control-plane.
 */
data class SubscriptionServiceReport(
    val serviceVersion: String?,
    val appliedRevision: Long?,
    val recoveryWatcherReady: Boolean?,
    val startedAt: Instant?,
)

/** Конфигурация в том виде, в каком её потребляет subscribe. */
data class SubscriptionServiceConfig(
    val revision: Long,
    val enabled: Boolean,
    val serviceName: String,
    val icon: String,
    val publicUrl: String,
    val yandexEditorUrl: String,
    val recoveryKeyId: String,
    val recoveryPrivateKey: String,
    val apps: List<SubscriptionApp>,
    /** true ровно в той выдаче, которая несёт запрошенный оператором перезапуск. */
    val restartRequested: Boolean,
)

interface SubscriptionServiceRepository {
    fun settings(): SubscriptionServiceSettings
    fun status(): SubscriptionServiceStatus
    fun replace(update: SubscriptionServiceUpdate, revision: Long, expectedRevision: Long, at: Instant): Boolean
    fun recoveryPrivateKey(): String?
    fun saveRecoveryKeys(privateKey: String, publicKey: String, at: Instant)
    fun bumpRestartNonce(at: Instant): Long
    fun attachToken(tokenId: String?, at: Instant)
    fun tokenId(): String?
    fun recordReport(report: SubscriptionServiceReport, at: Instant)

    /** Перезапуск отдаётся один раз: потеря доставки лечится повторным нажатием, а не циклом. */
    fun markRestartDelivered(nonce: Long, at: Instant)
}

interface SubscriptionServiceCommands {
    fun update(update: SubscriptionServiceUpdate, expectedRevision: Long, actor: String): SubscriptionServiceSettings
    fun issueToken(actor: String): IssuedServiceToken
    fun requestRestart(actor: String)
}

interface SubscriptionServiceQueries {
    fun settings(): SubscriptionServiceSettings
    fun status(): SubscriptionServiceStatus

    /** null, когда ревизия у сервиса уже актуальна: перекачивать нечего. */
    fun poll(report: SubscriptionServiceReport, since: Long?): SubscriptionServiceConfig?
}
