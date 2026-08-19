package io.boardproxy.control.activity.api.rest

import io.boardproxy.control.shared.events.ControlEvent
import io.boardproxy.control.shared.events.LocalControlEventBus
import jakarta.annotation.PostConstruct
import jakarta.annotation.PreDestroy
import org.slf4j.LoggerFactory
import org.springframework.scheduling.annotation.Scheduled
import org.springframework.stereotype.Component
import org.springframework.web.servlet.mvc.method.annotation.SseEmitter
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit

/**
 * Раздача событий в панель.
 *
 * Одна подписка на шину и один поток рассылки на всех, а не пул из одного
 * потока и отдельная подписка на каждое соединение: панель открывают единицы
 * операторов, и держать под каждого свой исполнитель было чистым расточительством.
 *
 * Очередь ограничена: если приёмник не успевает, события теряются, но рассылку
 * остальным это не задерживает. Панель живёт обновлениями состояния, а не
 * полнотой потока.
 */
@Component
class FrontendEventStream(private val events: LocalControlEventBus) {
    private val clients = ConcurrentHashMap.newKeySet<SseEmitter>()
    private val queue = ArrayBlockingQueue<ControlEvent>(QUEUE_CAPACITY)

    private var subscription: AutoCloseable? = null
    private var pump: Thread? = null

    @PostConstruct
    fun start() {
        subscription = events.subscribe { event -> queue.offer(event) }
        pump = Thread.ofVirtual().name("frontend-sse-pump").start {
            while (!Thread.currentThread().isInterrupted) {
                val event = runCatching { queue.poll(1, TimeUnit.SECONDS) }.getOrNull() ?: continue
                broadcast(event)
            }
        }
    }

    @PreDestroy
    fun stop() {
        subscription?.close()
        pump?.interrupt()
        clients.forEach { runCatching(it::complete) }
        clients.clear()
    }

    fun open(): SseEmitter {
        val emitter = SseEmitter(0L)
        clients += emitter
        emitter.onCompletion { clients -= emitter }
        emitter.onTimeout {
            clients -= emitter
            emitter.complete()
        }
        emitter.onError { clients -= emitter }
        runCatching { emitter.send(SseEmitter.event().name("ready").data(mapOf("ready" to true))) }
            .onFailure { clients -= emitter }
        return emitter
    }

    @Scheduled(fixedDelayString = "\${control.events.sse-heartbeat-delay:15000}")
    fun heartbeat() {
        clients.forEach { emitter ->
            runCatching { emitter.send(SseEmitter.event().comment("heartbeat")) }
                .onFailure { clients -= emitter }
        }
    }

    private fun broadcast(event: ControlEvent) {
        val payload = SseEmitter.event()
            .name(event.type)
            .data(
                mapOf(
                    "type" to event.type,
                    "aggregateId" to event.aggregateId,
                    "payload" to event.payload,
                    "occurredAt" to event.occurredAt,
                ),
            )
        clients.forEach { emitter ->
            runCatching { emitter.send(payload) }.onFailure { error ->
                clients -= emitter
                log.debug("dropped a frontend subscriber", error)
            }
        }
    }

    private companion object {
        const val QUEUE_CAPACITY = 1024
        val log = LoggerFactory.getLogger(FrontendEventStream::class.java)
    }
}
