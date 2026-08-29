package io.boardproxy.control.shared.agents

import java.time.Duration
import java.time.Instant

/**
 * Управляемый агент — внешний процесс, которому хаб выдаёт конфигурацию и от
 * которого получает отчёт о состоянии. Таких два вида: нода и сервис подписок.
 *
 * Общими у них сделаны хранилище состояния и семантика команд, но не транспорт:
 * нода говорит по mTLS/gRPC, subscribe — по bearer/HTTP. Абстрагировать ещё и
 * транспорт выгоды не даёт.
 */
enum class AgentKind {
    NODE,
    SUBSCRIPTION_SERVICE;

    fun databaseValue(): String = when (this) {
        NODE -> "node"
        SUBSCRIPTION_SERVICE -> "subscription-service"
    }

    companion object {
        fun parse(value: String): AgentKind = when (value) {
            "node" -> NODE
            "subscription-service" -> SUBSCRIPTION_SERVICE
            else -> error("unknown agent kind $value")
        }
    }
}

data class Agent(val id: String, val kind: AgentKind, val name: String)

/**
 * Наблюдаемое состояние агента.
 *
 * Флага «онлайн» здесь нет намеренно: он вычисляется при чтении из
 * [lastReportAt]. Прежняя схема хранила его колонкой и держала фоновую job,
 * которая раз в 15 секунд переписывала строки, чтобы флаг не устаревал.
 */
data class AgentStatus(
    val agentId: String,
    val bootId: String? = null,
    val seq: Long = 0,
    val appliedRevision: Long = 0,
    val appliedSha256: String? = null,
    val applyError: String? = null,
    val agentVersion: String? = null,
    val uptimeSeconds: Long? = null,
    val lastReportAt: Instant? = null,
    /** Поля, специфичные для вида агента: recoveryWatcherReady, startedAt и т.п. */
    val details: Map<String, Any?> = emptyMap(),
) {
    fun online(now: Instant, threshold: Duration = OFFLINE_AFTER): Boolean =
        lastReportAt != null && Duration.between(lastReportAt, now) < threshold

    companion object {
        val OFFLINE_AFTER: Duration = Duration.ofSeconds(45)
    }
}

data class AgentCommand(
    val agentId: String,
    val nonce: Long,
    val kind: String,
    val issuedBy: String,
    val issuedAt: Instant,
)

interface AgentRegistry {
    fun register(agent: Agent)
    fun find(id: String): Agent?
    fun list(): List<Agent>
}

interface AgentStatusRepository {
    fun find(agentId: String): AgentStatus?
    fun list(): List<AgentStatus>
    /** false — отчёт принадлежит устаревшему boot или имеет меньший seq. */
    fun record(status: AgentStatus): Boolean
}

/**
 * Команда доставляется ровно один раз. Агент не хранит состояние между
 * запусками: отчитавшись после рестарта, он получил бы ту же команду снова — и
 * так бесконечно. Поэтому факт доставки ведёт хаб, а потерянная доставка
 * лечится повторным нажатием оператора.
 */
interface AgentCommandRepository {
    fun issue(agentId: String, kind: String, issuedBy: String, at: Instant): Long
    fun pending(agentId: String): AgentCommand?
    fun markDelivered(agentId: String, nonce: Long, at: Instant)
}

interface AgentReportLog {
    /** false — отчёт с этим batch_id уже принимали, обрабатывать повторно нельзя. */
    fun claim(agentId: String, batchId: String, at: Instant): Boolean
}
