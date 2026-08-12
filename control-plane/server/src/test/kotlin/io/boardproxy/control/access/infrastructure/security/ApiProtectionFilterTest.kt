package io.boardproxy.control.access.infrastructure.security

import jakarta.servlet.FilterChain
import org.springframework.mock.web.MockHttpServletRequest
import org.springframework.mock.web.MockHttpServletResponse
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import kotlin.test.Test
import kotlin.test.assertEquals

class ApiProtectionFilterTest {
    private val clock = Clock.fixed(Instant.parse("2026-08-12T12:00:00Z"), ZoneOffset.UTC)

    @Test
    fun `rejects declared oversized request before controller`() {
        val filter = ApiProtectionFilter(10, 3, clock)
        val request = MockHttpServletRequest("POST", "/api/v1/catalogs").apply { setContent(byteArrayOf(1, 2, 3, 4)) }
        val response = MockHttpServletResponse()
        var called = false

        filter.doFilter(request, response, FilterChain { _, _ -> called = true })

        assertEquals(false, called)
        assertEquals(413, response.status)
    }

    @Test
    fun `rate limit is isolated by bearer identity`() {
        val filter = ApiProtectionFilter(1, 10, clock)
        fun request(token: String): Int {
            val value = MockHttpServletRequest("GET", "/api/v1/nodes").apply { addHeader("Authorization", "Bearer $token") }
            val response = MockHttpServletResponse()
            filter.doFilter(value, response, FilterChain { _, output -> (output as MockHttpServletResponse).status = 204 })
            return response.status
        }

        assertEquals(204, request("one"))
        assertEquals(429, request("one"))
        assertEquals(204, request("two"))
    }
}
