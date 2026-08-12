package io.boardproxy.control.access.infrastructure.security

import io.boardproxy.control.access.application.AccessAuthenticator
import jakarta.servlet.FilterChain
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletResponse
import org.springframework.http.MediaType
import org.springframework.security.authentication.UsernamePasswordAuthenticationToken
import org.springframework.security.core.authority.SimpleGrantedAuthority
import org.springframework.security.core.context.SecurityContextHolder
import org.springframework.web.filter.OncePerRequestFilter

class BearerTokenAuthenticationFilter(private val authenticator: AccessAuthenticator) : OncePerRequestFilter() {
    override fun doFilterInternal(
        request: HttpServletRequest,
        response: HttpServletResponse,
        filterChain: FilterChain,
    ) {
        val header = request.getHeader("Authorization")
        if (header == null) {
            filterChain.doFilter(request, response)
            return
        }
        if (!header.startsWith(BEARER_PREFIX) || header.length == BEARER_PREFIX.length) {
            unauthorized(response)
            return
        }
        val principal = authenticator.authenticate(header.substring(BEARER_PREFIX.length))
        if (principal == null) {
            unauthorized(response)
            return
        }
        val authentication = UsernamePasswordAuthenticationToken.authenticated(
            principal.name,
            null,
            principal.role.authorities().map(::SimpleGrantedAuthority),
        )
        val context = SecurityContextHolder.createEmptyContext().also { it.authentication = authentication }
        SecurityContextHolder.setContext(context)
        try {
            filterChain.doFilter(request, response)
        } finally {
            SecurityContextHolder.clearContext()
        }
    }

    private fun unauthorized(response: HttpServletResponse) {
        response.status = HttpServletResponse.SC_UNAUTHORIZED
        response.contentType = MediaType.APPLICATION_PROBLEM_JSON_VALUE
        response.writer.write("""{"status":401,"title":"Unauthorized"}""")
    }

    private companion object {
        const val BEARER_PREFIX = "Bearer "
    }
}
