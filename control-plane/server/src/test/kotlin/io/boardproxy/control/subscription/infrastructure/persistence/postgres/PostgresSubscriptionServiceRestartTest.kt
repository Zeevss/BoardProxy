package io.boardproxy.control.subscription.infrastructure.persistence.postgres

import io.boardproxy.control.shared.agents.postgres.PostgresAgentCommandRepository
import io.boardproxy.control.shared.agents.postgres.PostgresAgentRegistry
import io.boardproxy.control.shared.agents.postgres.PostgresAgentStatusRepository
import io.boardproxy.control.testing.PostgresSupport
import io.boardproxy.control.testing.TEST_TIME
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * Доставка запроса на перезапуск сервиса подписок.
 *
 * Кнопку в панели жмут повторно, когда кажется, что не сработало. Каждое
 * нажатие заводит команду, а подтверждалась только последняя: старшие
 * оставались ждущими, и сервис на каждом опросе получал приказ снова —
 * `restart: unless-stopped` крутил его по кругу.
 */
class PostgresSubscriptionServiceRestartTest {
    private val agents = PostgresAgentRegistry(PostgresSupport.named)
    private val statuses = PostgresAgentStatusRepository(PostgresSupport.named, PostgresSupport.json)
    private val commands = PostgresAgentCommandRepository(PostgresSupport.named)
    private val repository = PostgresSubscriptionServiceRepository(
        PostgresSupport.named, PostgresSupport.cipher, PostgresSupport.json, agents, statuses, commands,
    )

    @BeforeTest
    fun prepare() {
        assertTrue(PostgresSupport.dockerAvailable, "тесты репозиториев требуют Docker")
        PostgresSupport.truncate()
        // Единственную строку настроек заводит миграция, но truncate доходит до
        // неё каскадом через credentials — возвращаем ровно так же, как V1.
        PostgresSupport.jdbc.update(
            "INSERT INTO subscription_service_settings (id) VALUES (true) ON CONFLICT DO NOTHING",
        )
    }

    @Test
    fun `один перезапуск гасит все накопившиеся запросы`() {
        repeat(4) { repository.bumpRestartNonce(TEST_TIME) }
        val requested = repository.settings().restartNonce
        assertEquals(4, requested)
        assertTrue(repository.status().ackedRestartNonce < requested, "перезапуск обязан ждать доставки")

        repository.markRestartDelivered(requested, TEST_TIME)

        assertEquals(
            requested,
            repository.status().ackedRestartNonce,
            "после доставки не должно остаться ждущих запросов, иначе сервис зациклится",
        )
        assertEquals(0, pendingCount(), "ни одна команда не должна остаться невыданной")
    }

    @Test
    fun `новый запрос после доставки снова ждёт`() {
        repository.bumpRestartNonce(TEST_TIME)
        repository.markRestartDelivered(repository.settings().restartNonce, TEST_TIME)

        repository.bumpRestartNonce(TEST_TIME)

        assertTrue(
            repository.status().ackedRestartNonce < repository.settings().restartNonce,
            "нажатие после доставки обязано снова запросить перезапуск",
        )
    }

    private fun pendingCount(): Int = PostgresSupport.jdbc.queryForObject(
        "SELECT COUNT(*) FROM agent_commands WHERE agent_id = 'subscription-service' AND delivered_at IS NULL",
        Int::class.java,
    ) ?: 0
}
