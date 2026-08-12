package io.boardproxy.control.access.api.rest

import io.boardproxy.control.access.application.ApiTokenCommands
import io.boardproxy.control.access.application.ApiTokenQueries
import io.boardproxy.control.access.domain.AccessRole
import io.boardproxy.control.access.domain.ApiToken
import org.springframework.http.HttpStatus
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.DeleteMapping
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.ResponseStatus
import org.springframework.web.bind.annotation.RestController
import java.security.Principal
import java.time.Duration
import java.time.Instant

@RestController
@RequestMapping("/api/v1/access/tokens")
@PreAuthorize("hasRole('ADMIN')")
class ApiTokenController(
    private val commands: ApiTokenCommands,
    private val queries: ApiTokenQueries,
) {
    @PostMapping
    @ResponseStatus(HttpStatus.CREATED)
    fun issue(@RequestBody request: IssueApiTokenRequest, principal: Principal): IssuedApiTokenResponse {
        val issued = commands.issue(
            request.name,
            request.role,
            request.ttlSeconds?.let(Duration::ofSeconds),
            principal.name,
        )
        return IssuedApiTokenResponse(issued.token.toResponse(), issued.secret)
    }

    @GetMapping
    fun list(): List<ApiTokenResponse> = queries.list().map(ApiToken::toResponse)

    @DeleteMapping("/{id}")
    @ResponseStatus(HttpStatus.NO_CONTENT)
    fun revoke(@PathVariable id: String, principal: Principal) {
        commands.revoke(id, principal.name)
    }
}

data class IssueApiTokenRequest(val name: String, val role: AccessRole, val ttlSeconds: Long? = null)

data class IssuedApiTokenResponse(val token: ApiTokenResponse, val secret: String)

data class ApiTokenResponse(
    val id: String,
    val name: String,
    val role: AccessRole,
    val createdBy: String,
    val createdAt: Instant,
    val expiresAt: Instant?,
    val revokedAt: Instant?,
    val lastUsedAt: Instant?,
)

private fun ApiToken.toResponse() = ApiTokenResponse(
    id, name, role, createdBy, createdAt, expiresAt, revokedAt, lastUsedAt,
)
