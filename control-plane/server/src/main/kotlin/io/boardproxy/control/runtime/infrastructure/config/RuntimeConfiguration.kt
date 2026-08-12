package io.boardproxy.control.runtime.infrastructure.config

import io.boardproxy.control.runtime.application.RuntimeEventService
import io.boardproxy.control.runtime.application.RuntimeEventStore
import io.boardproxy.control.runtime.application.RuntimeProjectionNotifier
import io.boardproxy.control.runtime.application.RuntimeProjectionRebuildService
import io.boardproxy.control.runtime.application.RuntimeReplayStore
import io.boardproxy.control.shared.persistence.TransactionRunner
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration

@Configuration
class RuntimeConfiguration {
    @Bean
    fun runtimeEventService(
        store: RuntimeEventStore,
        transactions: TransactionRunner,
        notifier: RuntimeProjectionNotifier,
    ) = RuntimeEventService(store, transactions, notifier)

    @Bean
    fun runtimeProjectionRebuildService(
        store: RuntimeEventStore,
        replay: RuntimeReplayStore,
        transactions: TransactionRunner,
        notifier: RuntimeProjectionNotifier,
    ) = RuntimeProjectionRebuildService(store, replay, transactions, notifier)
}
