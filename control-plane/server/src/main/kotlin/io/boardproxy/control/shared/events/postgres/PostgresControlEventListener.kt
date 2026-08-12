package io.boardproxy.control.shared.events.postgres

import com.fasterxml.jackson.databind.ObjectMapper
import io.boardproxy.control.shared.events.ControlEvent
import io.boardproxy.control.shared.events.LocalControlEventBus
import io.boardproxy.control.shared.events.OutboxDeliveryRepository
import org.postgresql.PGConnection
import org.slf4j.LoggerFactory
import org.springframework.context.SmartLifecycle
import org.springframework.stereotype.Component
import java.sql.Connection
import javax.sql.DataSource

@Component
class PostgresControlEventListener(
    private val dataSource: DataSource,
    private val json: ObjectMapper,
    private val outbox: OutboxDeliveryRepository,
    private val localEvents: LocalControlEventBus,
) : SmartLifecycle {
    @Volatile
    private var running = false
    @Volatile
    private var connection: Connection? = null
    private var listenerThread: Thread? = null

    override fun start() {
        if (running) return
        running = true
        listenerThread = Thread.ofVirtual().name("postgres-control-events").start(::listen)
    }

    override fun stop() {
        running = false
        runCatching { connection?.close() }
        listenerThread?.interrupt()
        listenerThread = null
    }

    override fun isRunning(): Boolean = running

    override fun isAutoStartup(): Boolean = true

    override fun getPhase(): Int = 0

    private fun listen() {
        while (running) {
            try {
                dataSource.connection.use { opened ->
                    connection = opened
                    opened.createStatement().use { it.execute("LISTEN ${PostgresOutboxRepository.CHANNEL}") }
                    val postgres = opened.unwrap(PGConnection::class.java)
                    while (running && !opened.isClosed) {
                        postgres.getNotifications(NOTIFICATION_TIMEOUT_MILLIS)?.forEach { notification ->
                            dispatch(notification.parameter)
                        }
                    }
                }
            } catch (error: Exception) {
                if (running) {
                    logger.warn("PostgreSQL control-event listener disconnected; reconnecting", error)
                    try {
                        Thread.sleep(RECONNECT_DELAY_MILLIS)
                    } catch (_: InterruptedException) {
                        Thread.currentThread().interrupt()
                    }
                }
            } finally {
                connection = null
            }
        }
    }

    private fun dispatch(payload: String) {
        runCatching {
            val envelope = json.readValue(payload, PostgresOutboxRepository.NotificationEnvelope::class.java)
            when (envelope.kind) {
                "outbox" -> envelope.eventId?.let(outbox::find)?.let { event ->
                    localEvents.publish(ControlEvent(event.type, event.aggregateId, event.payload, event.occurredAt))
                }
                "realtime" -> envelope.event?.let(localEvents::publish)
                else -> logger.warn("ignoring unknown control-event envelope kind {}", envelope.kind)
            }
        }.onFailure { error -> logger.warn("failed to decode PostgreSQL control event", error) }
    }

    private companion object {
        const val NOTIFICATION_TIMEOUT_MILLIS = 5_000
        const val RECONNECT_DELAY_MILLIS = 1_000L
        val logger = LoggerFactory.getLogger(PostgresControlEventListener::class.java)
    }
}
