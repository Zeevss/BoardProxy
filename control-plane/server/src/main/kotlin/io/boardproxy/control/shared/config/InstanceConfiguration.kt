package io.boardproxy.control.shared.config

import org.springframework.beans.factory.annotation.Value
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import java.util.UUID

data class ControlInstanceId(val value: String)

@Configuration
class InstanceConfiguration {
    @Bean
    fun controlInstanceId(
        @Value("\${control.instance-id:}") configured: String,
    ) = ControlInstanceId(configured.trim().ifEmpty { UUID.randomUUID().toString() })
}
