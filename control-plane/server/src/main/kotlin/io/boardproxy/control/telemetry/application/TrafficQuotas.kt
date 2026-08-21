package io.boardproxy.control.telemetry.application

import io.boardproxy.control.shared.contracts.QuotaExceededQueries
import io.boardproxy.control.shared.contracts.UserQuotaSummary
import io.boardproxy.control.shared.contracts.UserQuotaSummaryQueries
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.errors.ResourceNotFound
import io.boardproxy.control.shared.events.OutboxEvent
import io.boardproxy.control.shared.events.OutboxRepository
import io.boardproxy.control.telemetry.domain.QuotaAction
import io.boardproxy.control.telemetry.domain.QuotaPeriod
import io.boardproxy.control.telemetry.domain.TrafficQuota
import io.boardproxy.control.telemetry.domain.TrafficQuotaState
import io.boardproxy.control.telemetry.domain.TrafficQuotaUsage
import java.time.Clock
import java.time.DayOfWeek
import java.time.Instant
import java.time.ZoneOffset
import java.time.ZonedDateTime
import java.util.UUID

interface TrafficQuotaRepository {
    fun find(userId: String): TrafficQuota?
    fun list(): List<TrafficQuota>
    fun save(quota: TrafficQuota, expectedVersion: Long?): Boolean
    fun delete(userId: String, expectedVersion: Long): Boolean
    fun enabled(): List<TrafficQuota>

    /** Суммарный расход по всем нодам: пользователь один, значит и счётчик один. */
    fun usedBytes(userId: String, from: Instant, to: Instant): Long

    fun state(userId: String): TrafficQuotaState?

    /** true — флаг [TrafficQuotaState.exceeded] изменился, конфигурацию надо пересобрать. */
    fun saveState(state: TrafficQuotaState): Boolean

    fun exceededUsers(): Set<String>
    fun startNewCounter(userId: String, at: Instant)
}

/** Узкий порт для потребителей вне telemetry: пользователь заводится вместе с квотой. */
fun interface TrafficQuotaCommands {
    fun put(
        userId: String,
        period: QuotaPeriod,
        limitBytes: Long,
        action: QuotaAction,
        enabled: Boolean,
        expectedVersion: Long?,
    ): TrafficQuota
}

fun interface TrafficQuotaNotifier {
    fun exceeded(usage: TrafficQuotaUsage)
}

/**
 * Телеметрия не пишет в desired state.
 *
 * Единственный её выход наружу — флаг [TrafficQuotaState.exceeded] и событие
 * `quota.changed`, по которому подписчик пересобирает конфигурацию затронутых
 * нод. Поэтому поведение симметрично: наступил новый период — флаг снялся,
 * конфигурация пересобралась, пользователь снова работает. Прежняя реализация
 * писала DISABLED прямо в каталог, и включать приходилось руками.
 */
class TrafficQuotaService(
    private val quotas: TrafficQuotaRepository,
    private val notifier: TrafficQuotaNotifier,
    private val outbox: OutboxRepository,
    private val clock: Clock,
    private val nextId: () -> String = { UUID.randomUUID().toString() },
) : TrafficQuotaCommands, QuotaExceededQueries, UserQuotaSummaryQueries {

    override fun exceededUsers(): Set<String> = quotas.exceededUsers()

    /**
     * Пользователи без квоты в карту не попадают — для панели это и значит
     * «без ограничения».
     */
    override fun all(): Map<String, UserQuotaSummary> = quotas.list().associate { quota ->
        val usage = usage(quota)
        quota.userId to UserQuotaSummary(
            limitBytes = quota.limitBytes,
            usedBytes = usage.usedBytes,
            exceeded = usage.exceeded,
            enabled = quota.enabled,
            periodEnd = usage.periodEnd,
        )
    }

    override fun put(
        userId: String,
        period: QuotaPeriod,
        limitBytes: Long,
        action: QuotaAction,
        enabled: Boolean,
        expectedVersion: Long?,
    ): TrafficQuota {
        val current = quotas.find(userId)
        if (current == null && expectedVersion != null) throw ResourceConflict("traffic quota does not exist")
        if (current != null && current.version != expectedVersion) throw ResourceConflict("traffic quota version changed")
        val value = TrafficQuota(
            userId, period, limitBytes, action, enabled,
            version = (current?.version ?: 0) + 1, updatedAt = clock.instant(),
            // Правка лимита не должна дарить новый цикл; устаревший сброс всё
            // равно отсекается началом периода.
            counterStart = current?.counterStart,
        )
        if (!quotas.save(value, expectedVersion)) throw ResourceConflict("traffic quota version changed")
        return value
    }

    fun get(userId: String): TrafficQuotaUsage? = quotas.find(userId)?.let(::usage)

    fun list(): List<TrafficQuotaUsage> = quotas.list().map(::usage)

    fun delete(userId: String, expectedVersion: Long) {
        if (!quotas.delete(userId, expectedVersion)) throw ResourceNotFound("traffic quota not found or changed")
    }

    fun evaluate() {
        val now = clock.instant()
        quotas.enabled().forEach { quota ->
            val usage = usage(quota)
            // Конфигурацию меняет только политика DISABLE: ALERT уведомляет,
            // RESET обслуживает дальше с нуля.
            val blocking = usage.exceeded && quota.action == QuotaAction.DISABLE
            val previous = quotas.state(quota.userId)

            if (quotas.saveState(TrafficQuotaState(quota.userId, usage.periodStart, usage.usedBytes, blocking, now))) {
                outbox.append(
                    OutboxEvent(
                        id = nextId(), aggregateType = "user", aggregateId = quota.userId,
                        type = "quota.changed",
                        payload = mapOf("userId" to quota.userId, "exceeded" to blocking),
                        occurredAt = now,
                    ),
                )
            }

            if (usage.exceeded && previous?.exceeded != true) notifier.exceeded(usage)
            if (usage.exceeded && quota.action == QuotaAction.RESET) quotas.startNewCounter(quota.userId, now)
        }
    }

    private fun usage(quota: TrafficQuota): TrafficQuotaUsage {
        val now = clock.instant()
        val (periodStart, end) = period(quota.period, now)
        // Сброшенный счётчик сдвигает начало отсчёта внутрь периода, но окно самого периода не меняет.
        val countFrom = maxOf(periodStart, quota.counterStart ?: periodStart)
        val used = quotas.usedBytes(quota.userId, countFrom, minOf(end, now))
        return TrafficQuotaUsage(quota, periodStart, end, used, used >= quota.limitBytes)
    }

    private fun period(period: QuotaPeriod, now: Instant): Pair<Instant, Instant> {
        if (period == QuotaPeriod.NONE) return Instant.EPOCH to LIFETIME_END
        val utc = ZonedDateTime.ofInstant(now, ZoneOffset.UTC)
        val start = when (period) {
            QuotaPeriod.DAILY -> utc.toLocalDate().atStartOfDay(ZoneOffset.UTC)
            QuotaPeriod.WEEKLY -> utc.with(DayOfWeek.MONDAY).toLocalDate().atStartOfDay(ZoneOffset.UTC)
            QuotaPeriod.MONTHLY -> utc.withDayOfMonth(1).toLocalDate().atStartOfDay(ZoneOffset.UTC)
            QuotaPeriod.NONE -> error("lifetime period is handled above")
        }
        val end = when (period) {
            QuotaPeriod.DAILY -> start.plusDays(1)
            QuotaPeriod.WEEKLY -> start.plusWeeks(1)
            QuotaPeriod.MONTHLY -> start.plusMonths(1)
            QuotaPeriod.NONE -> error("lifetime period is handled above")
        }
        return start.toInstant() to end.toInstant()
    }

    private companion object {
        /** Верхняя граница «без сброса»: конкретная дата вместо Instant.MAX, чтобы ложиться в timestamptz. */
        val LIFETIME_END: Instant = Instant.parse("9999-01-01T00:00:00Z")
    }
}
