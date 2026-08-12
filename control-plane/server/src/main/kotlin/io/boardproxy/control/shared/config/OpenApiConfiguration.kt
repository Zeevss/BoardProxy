package io.boardproxy.control.shared.config

import io.swagger.v3.oas.models.Components
import io.swagger.v3.oas.models.OpenAPI
import io.swagger.v3.oas.models.info.Info
import io.swagger.v3.oas.models.security.SecurityRequirement
import io.swagger.v3.oas.models.security.SecurityScheme
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration

@Configuration
class OpenApiConfiguration {
    @Bean
    fun controlPlaneOpenApi(): OpenAPI = OpenAPI()
        .info(
            Info()
                .title("BoardProxy Control Plane API")
                .version("v1")
                .description("Browser/control API. Node gRPC is a separate contract."),
        )
        .components(
            Components().addSecuritySchemes(
                "bearerAuth",
                SecurityScheme().type(SecurityScheme.Type.HTTP).scheme("bearer").bearerFormat("BoardProxy API token"),
            ),
        )
        .addSecurityItem(SecurityRequirement().addList("bearerAuth"))
}
