package io.boardproxy.control.activity.api.rest

import io.boardproxy.control.shared.events.ControlEvent
import io.boardproxy.control.shared.events.LocalControlEventBus
import org.springframework.scheduling.annotation.Scheduled
import org.springframework.stereotype.Component
import org.springframework.web.servlet.mvc.method.annotation.SseEmitter
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.ExecutorService
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit

@Component
class FrontendEventStream(private val events: LocalControlEventBus) {
    private val clients = ConcurrentHashMap<SseEmitter, Client>()

    fun open(): SseEmitter {
        val emitter = SseEmitter(0L)
        val executor = ThreadPoolExecutor(
            1, 1, 0, TimeUnit.MILLISECONDS, ArrayBlockingQueue(CLIENT_QUEUE_CAPACITY),
            Thread.ofVirtual().name("frontend-sse-", 0).factory(),
            ThreadPoolExecutor.AbortPolicy(),
        )
        val subscription = events.subscribe { event ->
            runCatching { executor.submit { send(emitter, event) } }
        }
        clients[emitter] = Client(subscription, executor)
        emitter.onCompletion { remove(emitter) }
        emitter.onTimeout {
            remove(emitter)
            emitter.complete()
        }
        emitter.onError { remove(emitter) }
        runCatching { emitter.send(SseEmitter.event().name("ready").data(mapOf("ready" to true))) }
            .onFailure { remove(emitter) }
        return emitter
    }

    @Scheduled(fixedDelayString = "\${control.events.sse-heartbeat-delay:15000}")
    fun heartbeat() {
        clients.forEach { (emitter, client) ->
            runCatching {
                client.executor.submit {
                    runCatching { emitter.send(SseEmitter.event().comment("heartbeat")) }
                        .onFailure { remove(emitter) }
                }
            }.onFailure { remove(emitter) }
        }
    }

    private fun send(emitter: SseEmitter, event: ControlEvent) {
        runCatching {
            emitter.send(
                SseEmitter.event()
                    .name(event.type)
                    .data(
                        mapOf(
                            "type" to event.type,
                            "aggregateId" to event.aggregateId,
                            "payload" to event.payload,
                            "occurredAt" to event.occurredAt,
                        ),
                    ),
            )
        }.onFailure { remove(emitter) }
    }

    private fun remove(emitter: SseEmitter) {
        clients.remove(emitter)?.let { client ->
            runCatching(client.subscription::close)
            client.executor.shutdownNow()
        }
    }

    private data class Client(val subscription: AutoCloseable, val executor: ExecutorService)

    private companion object {
        const val CLIENT_QUEUE_CAPACITY = 256
    }
}
