package io.boardproxy.control.access.infrastructure.security

import io.boardproxy.control.access.application.AccessAuthenticator
import io.boardproxy.control.access.domain.AccessPrincipal
import io.boardproxy.control.access.domain.AccessRole
import jakarta.servlet.FilterChain
import org.springframework.mock.web.MockHttpServletRequest
import org.springframework.mock.web.MockHttpServletResponse
import org.springframework.security.core.Authentication
import org.springframework.security.core.context.SecurityContextHolder
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class BearerTokenAuthenticationFilterTest {
    @Test
    fun `valid bearer token installs expanded role authorities for request only`() {
        val filter = BearerTokenAuthenticationFilter(
            AccessAuthenticator { AccessPrincipal("operator", AccessRole.OPERATOR) },
        )
        val request = MockHttpServletRequest().apply { addHeader("Authorization", "Bearer secret") }
        val response = MockHttpServletResponse()
        var captured: Authentication? = null

        filter.doFilter(request, response, FilterChain { _, _ ->
            captured = SecurityContextHolder.getContext().authentication
        })

        assertEquals("operator", captured?.name)
        assertEquals(setOf("ROLE_VIEWER", "ROLE_OPERATOR"), captured?.authorities?.map { it.authority }?.toSet())
        assertNull(SecurityContextHolder.getContext().authentication)
    }

    @Test
    fun `invalid bearer token is rejected before controller`() {
        val filter = BearerTokenAuthenticationFilter(AccessAuthenticator { null })
        val request = MockHttpServletRequest().apply { addHeader("Authorization", "Bearer invalid") }
        val response = MockHttpServletResponse()
        var invoked = false

        filter.doFilter(request, response, FilterChain { _, _ -> invoked = true })

        assertEquals(401, response.status)
        assertEquals(false, invoked)
    }
}
