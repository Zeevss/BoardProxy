package io.boardproxy.control.delivery.application

import java.time.Duration

/**
 * Подписка на изменения желаемой конфигурации ноды.
 *
 * Блокирующая, а не реактивная: обработчик Watch живёт на виртуальном потоке,
 * и ждать сигнала ему дешевле, чем городить корутины ради того же самого.
 */
interface RevisionSubscription : AutoCloseable {
    /**
     * Ждёт сигнала об изменении. false — истёк таймаут; вызывающий на всякий
     * случай повторяет текущую ревизию, чтобы потерянное уведомление не
     * оставило ноду со старой конфигурацией навсегда.
     */
    fun await(timeout: Duration): Boolean
}

fun interface DesiredRevisionSignals {
    fun subscribe(nodeId: String): RevisionSubscription
}
