package io.boardproxy.control.access.infrastructure.security

import jakarta.servlet.DispatcherType
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.boot.test.context.SpringBootTest
import org.springframework.mock.web.MockFilterChain
import org.springframework.mock.web.MockHttpServletRequest
import org.springframework.mock.web.MockHttpServletResponse
import org.springframework.security.web.FilterChainProxy
import org.springframework.test.context.DynamicPropertyRegistry
import org.springframework.test.context.DynamicPropertySource
import org.testcontainers.junit.jupiter.Container
import org.testcontainers.junit.jupiter.Testcontainers
import org.testcontainers.postgresql.PostgreSQLContainer
import java.nio.file.Files
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertNull

/**
 * Авторизация на продолжении запроса.
 *
 * Единственный асинхронный эндпоинт панели — SSE-поток. Spring Security с
 * шестой версии применяет авторизацию ко всем типам диспатча, а аутентификация
 * живёт в `OncePerRequestFilter`, который на ASYNC не повторяется: контекст
 * пуст, и продолжение уже разрешённого запроса получает отказ. Ответ к этому
 * моменту отправлен, записать в него 403 нельзя — в лог сыпались «Access
 * Denied» и «response is already committed».
 */
@SpringBootTest(
    properties = [
        "control.grpc.port=0",
        "control.access.bootstrap-admin-token=async-dispatch-test-token",
        "control.events.outbox-delay=PT1H",
        "control.events.sse-heartbeat-delay=PT1H",
        "control.delivery.status-expiry-delay=PT1H",
        "control.telemetry.rollup-delay=PT1H",
        "control.telemetry.quota-delay=PT1H",
        "control.telemetry.quota-reconcile-delay=PT1H",
    ],
)
@Testcontainers(disabledWithoutDocker = true)
class AsyncDispatchAuthorizationTest {
    @Autowired
    private lateinit var securityFilterChain: FilterChainProxy

    @Test
    fun `async dispatch continues the chain without re-authorising`() {
        val request = MockHttpServletRequest("GET", "/api/v1/events")
        request.dispatcherType = DispatcherType.ASYNC
        val response = MockHttpServletResponse()
        val chain = MockFilterChain()

        securityFilterChain.doFilter(request, response, chain)

        assertNotNull(chain.request, "продолжение запроса обязано дойти до конца цепочки")
        assertEquals(200, response.status)
    }

    /** Тот же путь на обычном диспатче остаётся закрытым. */
    @Test
    fun `plain request without a token is still refused`() {
        val request = MockHttpServletRequest("GET", "/api/v1/events")
        val response = MockHttpServletResponse()
        val chain = MockFilterChain()

        securityFilterChain.doFilter(request, response, chain)

        assertNull(chain.request, "неавторизованный запрос не должен доходить до контроллера")
        assertEquals(401, response.status)
    }

    companion object {
        private val pkiDirectory = Files.createTempDirectory("boardproxy-async-pki")

        @Container
        @JvmField
        val postgres: PostgreSQLContainer = PostgreSQLContainer("postgres:18-alpine")

        @JvmStatic
        @DynamicPropertySource
        fun properties(registry: DynamicPropertyRegistry) {
            registry.add("spring.datasource.url", postgres::getJdbcUrl)
            registry.add("spring.datasource.password", postgres::getPassword)
            registry.add("spring.datasource.username", postgres::getUsername)
            registry.add("control.security.master-key") { "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" }
            registry.add("control.security.master-key-id") { "test-v1" }
            registry.add("control.pki.directory") { pkiDirectory.toString() }
            registry.add("control.grpc.server-names") { "localhost" }
        }
    }
}
