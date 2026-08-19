package io.boardproxy.control.telemetry.application

import io.boardproxy.control.shared.events.OutboxEvent
import io.boardproxy.control.shared.events.OutboxRepository
import io.boardproxy.control.telemetry.domain.QuotaAction
import io.boardproxy.control.telemetry.domain.QuotaPeriod
import io.boardproxy.control.telemetry.domain.TrafficQuota
import io.boardproxy.control.telemetry.domain.TrafficQuotaState
import io.boardproxy.control.telemetry.domain.TrafficQuotaUsage
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * Телеметрия не должна писать в desired state. Её единственный выход — флаг
 * exceeded и событие quota.changed, поэтому проверяем именно их, а не побочные
 * эффекты в каталоге, как было раньше.
 */
class TrafficQuotaServiceTest {
    private val now = Instant.parse("2026-08-18T12:00:00Z")
    private val quotas = FakeQuotas()
    private val outbox = FakeOutbox()
    private val notifications = mutableListOf<TrafficQuotaUsage>()

    private val service = TrafficQuotaService(
        quotas = quotas,
        notifier = { notifications += it },
        outbox = outbox,
        clock = Clock.fixed(now, ZoneOffset.UTC),
        nextId = { "event-${outbox.events.size + 1}" },
    )

    @Test
    fun `превышение с политикой disable поднимает флаг и рождает событие`() {
        quotas.put(quota(QuotaAction.DISABLE, limit = 100))
        quotas.used = 150

        service.evaluate()

        assertTrue(quotas.state("u1")!!.exceeded)
        assertEquals(setOf("u1"), service.exceededUsers())
        assertEquals(listOf("quota.changed"), outbox.events.map(OutboxEvent::type))
        assertEquals(1, notifications.size)
    }

    /**
     * Симметрия — главное, ради чего убрана запись в каталог: новый период
     * снимает флаг сам, оператору не нужно включать пользователя руками.
     */
    @Test
    fun `снятие превышения возвращает пользователя в строй`() {
        quotas.put(quota(QuotaAction.DISABLE, limit = 100))
        quotas.used = 150
        service.evaluate()
        outbox.events.clear()

        quotas.used = 10
        service.evaluate()

        assertFalse(quotas.state("u1")!!.exceeded)
        assertEquals(emptySet(), service.exceededUsers())
        assertEquals(listOf("quota.changed"), outbox.events.map(OutboxEvent::type))
    }

    @Test
    fun `политика alert не влияет на конфигурацию`() {
        quotas.put(quota(QuotaAction.ALERT, limit = 100))
        quotas.used = 150

        service.evaluate()

        assertFalse(quotas.state("u1")!!.exceeded, "alert только уведомляет")
        assertEquals(emptySet(), service.exceededUsers())
        assertEquals(1, notifications.size)
    }

    @Test
    fun `неизменное состояние не порождает событий`() {
        quotas.put(quota(QuotaAction.DISABLE, limit = 100))
        quotas.used = 150
        service.evaluate()
        outbox.events.clear()

        service.evaluate()

        assertTrue(outbox.events.isEmpty(), "расход обновляется каждую минуту, событие — только на смену флага")
    }

    private fun quota(action: QuotaAction, limit: Long) = TrafficQuota(
        userId = "u1", period = QuotaPeriod.MONTHLY, limitBytes = limit,
        action = action, enabled = true, version = 1, updatedAt = now,
    )

    private class FakeQuotas : TrafficQuotaRepository {
        private val stored = mutableMapOf<String, TrafficQuota>()
        private val states = mutableMapOf<String, TrafficQuotaState>()
        var used: Long = 0

        fun put(quota: TrafficQuota) { stored[quota.userId] = quota }

        override fun find(userId: String) = stored[userId]
        override fun list() = stored.values.toList()
        override fun enabled() = stored.values.filter(TrafficQuota::enabled)
        override fun save(quota: TrafficQuota, expectedVersion: Long?): Boolean {
            stored[quota.userId] = quota
            return true
        }
        override fun delete(userId: String, expectedVersion: Long) = stored.remove(userId) != null
        override fun usedBytes(userId: String, from: Instant, to: Instant) = used
        override fun state(userId: String) = states[userId]
        override fun saveState(state: TrafficQuotaState): Boolean {
            val previous = states.put(state.userId, state)
            return previous == null || previous.exceeded != state.exceeded
        }
        override fun exceededUsers() = states.filterValues(TrafficQuotaState::exceeded).keys
        override fun startNewCounter(userId: String, at: Instant) {
            stored[userId] = stored.getValue(userId).copy(counterStart = at)
        }
    }

    private class FakeOutbox : OutboxRepository {
        val events = mutableListOf<OutboxEvent>()
        override fun append(event: OutboxEvent) { events += event }
    }
}
