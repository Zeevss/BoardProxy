package io.boardproxy.control.provisioning.infrastructure.events

import io.boardproxy.control.provisioning.application.DesiredConfigPublisher
import io.boardproxy.control.provisioning.application.GrantRepository
import io.boardproxy.control.shared.events.ControlEvent
import io.boardproxy.control.shared.events.LocalControlEventBus
import io.boardproxy.control.shared.persistence.TransactionRunner
import jakarta.annotation.PostConstruct
import jakarta.annotation.PreDestroy
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Component

/**
 * Замыкает квоту на конфигурацию.
 *
 * Телеметрия не знает ни о компиляторе, ни о нодах — она лишь сообщает, что у
 * пользователя изменился флаг превышения. Пересборкой занимается provisioning,
 * и только здесь, в инфраструктуре, эти два контекста встречаются.
 *
 * Событие приходит и при снятии превышения, поэтому новый период возвращает
 * пользователя в строй той же дорогой, что и выключил.
 */
@Component
class QuotaChangeSubscriber(
    private val events: LocalControlEventBus,
    private val grants: GrantRepository,
    private val publisher: DesiredConfigPublisher,
    private val transactions: TransactionRunner,
) {
    private var subscription: AutoCloseable? = null

    @PostConstruct
    fun start() {
        subscription = events.subscribe(::handle)
    }

    @PreDestroy
    fun stop() {
        subscription?.close()
        subscription = null
    }

    private fun handle(event: ControlEvent) {
        if (event.type != QUOTA_CHANGED) return
        val userId = event.payload["userId"] as? String ?: return
        val nodeIds = grants.nodesOf(userId)
        if (nodeIds.isEmpty()) return

        runCatching {
            transactions.required { publisher.publish(nodeIds, QUOTA_CHANGED, ACTOR) }
        }.onFailure { error ->
            logger.warn("failed to republish configuration after quota change for user {}", userId, error)
        }
    }

    private companion object {
        const val QUOTA_CHANGED = "quota.changed"
        const val ACTOR = "system:traffic-quota"
        val logger = LoggerFactory.getLogger(QuotaChangeSubscriber::class.java)
    }
}
