package io.boardproxy.control.provisioning.application

import io.boardproxy.control.shared.audit.AuditRepository
import io.boardproxy.control.shared.audit.AuditEvent
import io.boardproxy.control.shared.contracts.ControlTelemetry
import io.boardproxy.control.shared.contracts.QuotaExceededQueries
import io.boardproxy.control.shared.crypto.sha256Hex
import io.boardproxy.control.shared.events.OutboxEvent
import io.boardproxy.control.shared.events.OutboxRepository
import java.time.Clock
import java.util.UUID

data class PublishResult(
    val nodeId: String,
    val revision: Long,
    val configSha256: String,
    /** false — правка не изменила ни байта конфигурации, будить ноду незачем. */
    val changed: Boolean,
)

/**
 * Единственная точка записи desired state.
 *
 * Ревизия появляется только когда изменились байты TOML. Прежняя схема писала
 * ревизию на каждую замену каталога и определяла факт изменения косвенно —
 * сравнением версии каталога с версией в записанной ревизии, то есть по тому,
 * сработал ли ON CONFLICT по уникальному хешу. Здесь это прямое сравнение sha256.
 *
 * Множество затронутых нод передаётся явно вызывающим сервисом в той же
 * транзакции: правка пользователя задевает ноды его грантов, правка борда —
 * одну ноду. Фонового отслеживания «грязных» нод нет намеренно.
 */
class DesiredConfigPublisher(
    private val states: NodeStateLoader,
    private val quotas: QuotaExceededQueries,
    private val compiler: CoreConfigCompiler,
    private val configs: DesiredConfigRepository,
    private val snapshots: NodeSnapshotRepository,
    private val audit: AuditRepository,
    private val outbox: OutboxRepository,
    private val clock: Clock,
    private val telemetry: ControlTelemetry = ControlTelemetry.NOOP,
    private val nextId: () -> String = { UUID.randomUUID().toString() },
) {
    fun publish(nodeIds: Set<String>, cause: String, actor: String): List<PublishResult> {
        if (nodeIds.isEmpty()) return emptyList()
        val now = clock.instant()
        val exceeded = quotas.exceededUsers()

        return nodeIds.sorted().map { nodeId ->
            val state = states.load(nodeId)
            val toml = compiler.compile(state.withQuotas(exceeded))
            val sha256 = toml.sha256Hex()
            val current = configs.find(nodeId)

            if (current != null && current.configSha256 == sha256) {
                telemetry.configPublished(changed = false)
                return@map PublishResult(nodeId, current.revision, sha256, changed = false)
            }

            val revision = (current?.revision ?: 0L) + 1
            configs.save(DesiredConfig(nodeId, revision, sha256, toml, now))
            snapshots.save(state, cause, actor, now)
            audit.append(
                AuditEvent(
                    id = nextId(), nodeId = nodeId, actor = actor, action = cause,
                    resourceType = "node-config", resourceId = nodeId,
                    resourceVersion = revision, occurredAt = now,
                ),
            )
            outbox.append(
                OutboxEvent(
                    id = nextId(), aggregateType = "node", aggregateId = nodeId,
                    type = "desired-state.changed",
                    payload = mapOf("nodeId" to nodeId, "revision" to revision, "configSha256" to sha256),
                    occurredAt = now,
                ),
            )
            telemetry.configPublished(changed = true)
            PublishResult(nodeId, revision, sha256, changed = true)
        }
    }
}
