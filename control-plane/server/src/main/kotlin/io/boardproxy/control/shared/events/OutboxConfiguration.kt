package io.boardproxy.control.shared.events

import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration

@Configuration
class OutboxConfiguration {
    @Bean
    fun outboxOperations(repository: OutboxDeliveryRepository) = OutboxOperations(repository)
}
