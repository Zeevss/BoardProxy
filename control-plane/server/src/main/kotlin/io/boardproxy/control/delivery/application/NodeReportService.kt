package io.boardproxy.control.delivery.application

import io.boardproxy.control.shared.agents.AgentCommand
import io.boardproxy.control.shared.agents.AgentCommandRepository
import io.boardproxy.control.shared.agents.AgentReportLog
import io.boardproxy.control.shared.agents.AgentStatus
import io.boardproxy.control.shared.agents.AgentStatusRepository
import io.boardproxy.control.shared.contracts.ControlTelemetry
import io.boardproxy.control.shared.persistence.TransactionRunner
import java.time.Clock
import java.time.Instant

/** Одно наблюдение ноды: состояние применения плюс всё, что она накопила. */
data class NodeReport(
    val nodeId: String,
    val bootId: String,
    val seq: Long,
    val batchId: String,
    val appliedRevision: Long,
    val appliedSha256: String,
    val applyError: String,
    val coreVersion: String,
    val agentVersion: String,
    val uptimeSeconds: Long,
    val observedAt: Instant,
    val runtime: RuntimeSnapshotInput? = null,
    val interfaceTraffic: List<InterfaceTrafficInput> = emptyList(),
    val userTraffic: List<UserTrafficInput> = emptyList(),
    val events: List<RuntimeEventInput> = emptyList(),
)

data class RuntimeSnapshotInput(
    val coreBootId: String,
    val capturedAt: Instant,
    val users: List<RuntimeUserInput>,
    val boards: List<RuntimeBoardInput>,
)

data class RuntimeUserInput(
    val userId: String,
    val activeSessions: Int,
    val activeLanes: Int,
    val lastSeenAt: Instant?,
)

data class RuntimeBoardInput(
    val boardId: String,
    val state: String,
    val activeLanes: Int,
    val lastError: String,
)

data class InterfaceTrafficInput(
    val interfaceName: String,
    val rxBytes: Long,
    val txBytes: Long,
    val rxPackets: Long,
    val txPackets: Long,
    val rxErrors: Long,
    val txErrors: Long,
    val rxDropped: Long,
    val txDropped: Long,
    val observedAt: Instant,
)

data class UserTrafficInput(
    val userId: String,
    val rxBytes: Long,
    val txBytes: Long,
    val observedAt: Instant,
)

data class RuntimeEventInput(val type: String, val occurredAt: Instant, val payloadJson: String)

interface NodeTrafficSink {
    fun record(nodeId: String, batchId: String, interfaces: List<InterfaceTrafficInput>, users: List<UserTrafficInput>)
}

interface NodeRuntimeSink {
    fun replaceSnapshot(nodeId: String, snapshot: RuntimeSnapshotInput)
    fun appendEvents(nodeId: String, events: List<RuntimeEventInput>)
}

/**
 * Приём отчёта ноды.
 *
 * Обработчик без состояния: отчёт несёт всё, что нужно, поэтому хабу нечего
 * помнить между вызовами. Отсюда отсутствие лиза с fencing-токеном — поздний
 * запрос не может перезаписать свежий кэшем, которого нет.
 *
 * Повтор отсекается на входе одной вставкой: приём начинается с попытки занять
 * batch_id, и 0 строк означает, что этот отчёт уже обрабатывали.
 */
class NodeReportService(
    private val reports: AgentReportLog,
    private val statuses: AgentStatusRepository,
    private val commands: AgentCommandRepository,
    private val traffic: NodeTrafficSink,
    private val runtime: NodeRuntimeSink,
    private val transactions: TransactionRunner,
    private val clock: Clock,
    private val telemetry: ControlTelemetry = ControlTelemetry.NOOP,
) {
    fun accept(report: NodeReport): List<AgentCommand> = transactions.required {
        val now = clock.instant()
        val fresh = reports.claim(report.nodeId, report.batchId, now)
        telemetry.reportAccepted(fresh)

        if (fresh) {
            traffic.record(report.nodeId, report.batchId, report.interfaceTraffic, report.userTraffic)
            report.runtime?.let { runtime.replaceSnapshot(report.nodeId, it) }
            if (report.events.isNotEmpty()) runtime.appendEvents(report.nodeId, report.events)
        }

        // Состояние пишется даже для повтора: оно идемпотентно и отражает то,
        // что нода думает о себе прямо сейчас.
        statuses.record(
            AgentStatus(
                agentId = report.nodeId,
                bootId = report.bootId,
                seq = report.seq,
                appliedRevision = report.appliedRevision,
                appliedSha256 = report.appliedSha256.ifBlank { null },
                applyError = report.applyError.ifBlank { null },
                agentVersion = report.agentVersion.ifBlank { null },
                uptimeSeconds = report.uptimeSeconds.takeIf { it > 0 },
                lastReportAt = now,
                details = mapOf("coreVersion" to report.coreVersion.ifBlank { null }),
            ),
        )

        val pending = commands.pending(report.nodeId)
        if (pending != null) commands.markDelivered(report.nodeId, pending.nonce, now)
        listOfNotNull(pending)
    }
}
