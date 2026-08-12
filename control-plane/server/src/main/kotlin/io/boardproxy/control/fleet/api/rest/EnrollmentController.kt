package io.boardproxy.control.fleet.api.rest

import io.boardproxy.control.fleet.application.EnrollmentService
import org.springframework.http.HttpStatus
import org.springframework.security.access.prepost.PreAuthorize
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
@RequestMapping("/api/v1/nodes/{nodeId}/enrollment-tokens")
class EnrollmentController(private val enrollment: EnrollmentService) {
    @PostMapping
    @ResponseStatus(HttpStatus.CREATED)
    @PreAuthorize("hasAnyRole('OPERATOR', 'ADMIN')")
    fun issue(
        @PathVariable nodeId: String,
        @RequestBody request: IssueEnrollmentTokenRequest,
        principal: Principal,
    ): IssueEnrollmentTokenResponse {
        val (secret, expiresAt) = enrollment.issueBootstrap(
            nodeId, request.hubUrl, Duration.ofSeconds(request.ttlSeconds), principal.name,
        )
        return IssueEnrollmentTokenResponse(secret, expiresAt)
    }
}

data class IssueEnrollmentTokenRequest(val hubUrl: String, val ttlSeconds: Long = 900)
data class IssueEnrollmentTokenResponse(val nodeSecret: String, val expiresAt: Instant)
