package io.boardproxy.control.access.api.rest

import io.boardproxy.control.access.application.IssuedPanelSession
import io.boardproxy.control.access.application.PanelAuthOperations
import org.springframework.http.HttpStatus
import org.springframework.security.core.Authentication
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestBody
import org.springframework.web.bind.annotation.RequestHeader
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.ResponseStatus
import org.springframework.web.bind.annotation.RestController
import java.time.Instant

@RestController
@RequestMapping("/api/v1/auth")
class PanelAuthController(private val auth: PanelAuthOperations) {
    @GetMapping("/status")
    fun status() = PanelAuthStatusResponse(auth.status().setupRequired)

    @PostMapping("/setup")
    @ResponseStatus(HttpStatus.CREATED)
    fun setup(@RequestBody request: PanelCredentialsRequest) =
        auth.setup(request.username, request.password).response()

    @PostMapping("/login")
    fun login(@RequestBody request: PanelCredentialsRequest) =
        auth.login(request.username, request.password).response()

    @GetMapping("/me")
    fun me(authentication: Authentication) = PanelUserResponse(
        username = authentication.name,
        role = authentication.authorities.firstOrNull { it.authority == "ROLE_ADMIN" }
            ?.authority?.removePrefix("ROLE_") ?: "UNKNOWN",
    )

    @PostMapping("/logout")
    @ResponseStatus(HttpStatus.NO_CONTENT)
    fun logout(@RequestHeader("Authorization", required = false) authorization: String?) {
        auth.logout(authorization?.removePrefix("Bearer ").orEmpty())
    }
}

data class PanelAuthStatusResponse(val setupRequired: Boolean)
data class PanelCredentialsRequest(val username: String, val password: String)
data class PanelUserResponse(val username: String, val role: String)
data class PanelSessionResponse(
    val token: String,
    val expiresAt: Instant,
    val user: PanelUserResponse,
)

private fun IssuedPanelSession.response() = PanelSessionResponse(
    token = token,
    expiresAt = expiresAt,
    user = PanelUserResponse(username, "ADMIN"),
)
