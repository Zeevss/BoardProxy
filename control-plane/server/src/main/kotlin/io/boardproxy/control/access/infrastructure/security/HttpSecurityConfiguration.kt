package io.boardproxy.control.access.infrastructure.security

import io.boardproxy.control.access.application.AccessAuthenticator
import jakarta.servlet.http.HttpServletResponse
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import org.springframework.http.HttpMethod
import org.springframework.http.MediaType
import org.springframework.security.config.annotation.method.configuration.EnableMethodSecurity
import org.springframework.security.config.annotation.web.builders.HttpSecurity
import org.springframework.security.config.http.SessionCreationPolicy
import org.springframework.security.web.SecurityFilterChain
import org.springframework.security.web.authentication.UsernamePasswordAuthenticationFilter
import org.springframework.beans.factory.annotation.Value
import org.springframework.web.cors.CorsConfiguration
import org.springframework.web.cors.CorsConfigurationSource
import org.springframework.web.cors.UrlBasedCorsConfigurationSource
import java.time.Clock

@Configuration
@EnableMethodSecurity
class HttpSecurityConfiguration {
    @Bean
    fun apiProtectionFilter(
        clock: Clock,
        @Value("\${control.http.requests-per-minute:300}") requestsPerMinute: Int,
        @Value("\${control.http.maximum-body-bytes:2097152}") maximumBodyBytes: Long,
    ) = ApiProtectionFilter(requestsPerMinute, maximumBodyBytes, clock)

    @Bean
    fun corsConfigurationSource(
        @Value("\${control.http.allowed-origins:}") configured: String,
    ): CorsConfigurationSource {
        val configuration = CorsConfiguration().apply {
            allowedOrigins = configured.split(',').map(String::trim).filter(String::isNotEmpty)
            allowedMethods = listOf("GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS")
            allowedHeaders = listOf("Authorization", "Content-Type", "If-Match")
            exposedHeaders = listOf("ETag")
            allowCredentials = false
            maxAge = 3_600
        }
        return UrlBasedCorsConfigurationSource().also { it.registerCorsConfiguration("/**", configuration) }
    }
    @Bean
    fun bearerTokenAuthenticationFilter(authenticator: AccessAuthenticator) =
        BearerTokenAuthenticationFilter(authenticator)

    @Bean
    fun httpSecurityFilterChain(
        http: HttpSecurity,
        bearer: BearerTokenAuthenticationFilter,
        protection: ApiProtectionFilter,
    ): SecurityFilterChain {
        http
            .csrf { it.disable() }
            .cors { }
            .sessionManagement { it.sessionCreationPolicy(SessionCreationPolicy.STATELESS) }
            .requestCache { it.disable() }
            .formLogin { it.disable() }
            .httpBasic { it.disable() }
            .authorizeHttpRequests {
                it.requestMatchers(HttpMethod.OPTIONS, "/**").permitAll()
                    .requestMatchers("/", "/index.html", "/assets/**", "/favicon.ico", "/favicon.svg").permitAll()
                    .requestMatchers("/actuator/health", "/actuator/health/**").permitAll()
                    .requestMatchers("/v3/api-docs/**", "/swagger-ui/**", "/swagger-ui.html").permitAll()
                    .requestMatchers("/actuator/**").hasRole("ADMIN")
                    .anyRequest().authenticated()
            }
            .exceptionHandling {
                it.authenticationEntryPoint { _, response, _ -> problem(response, 401, "Unauthorized") }
                it.accessDeniedHandler { _, response, _ -> problem(response, 403, "Forbidden") }
            }
            .addFilterBefore(bearer, UsernamePasswordAuthenticationFilter::class.java)
            .addFilterBefore(protection, BearerTokenAuthenticationFilter::class.java)
        return http.build()
    }

    private fun problem(response: HttpServletResponse, status: Int, title: String) {
        response.status = status
        response.contentType = MediaType.APPLICATION_PROBLEM_JSON_VALUE
        response.writer.write("""{"status":$status,"title":"$title"}""")
    }
}
