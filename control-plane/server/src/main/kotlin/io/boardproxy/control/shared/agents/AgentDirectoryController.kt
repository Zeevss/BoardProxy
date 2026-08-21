package io.boardproxy.control.shared.agents

import io.boardproxy.control.shared.contracts.DesiredRevisionQueries
import io.boardproxy.control.shared.contracts.RuntimeTotalsQueries
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController
import java.time.Clock
import java.time.Instant

/**
 * Ноды и сервис подписок одним списком: у них общее понятие состояния, поэтому
 * и экран в панели общий.
 *
 * Здесь же лежит всё, что панель показывает в строке ноды. Раньше за ревизией
 * и сессиями приходилось идти в `/nodes/{id}/config` и `/nodes/{id}/runtime`
 * отдельно на каждую ноду — на пяти нодах это одиннадцать запросов вместо
 * одного, и при лимите в 300 запросов в минуту список упирался в него сам по
 * себе.
 */
@RestController
@RequestMapping("/api/v1/agents")
class AgentDirectoryController(
    private val agents: AgentRegistry,
    private val statuses: AgentStatusRepository,
    private val desired: DesiredRevisionQueries,
    private val runtime: RuntimeTotalsQueries,
    private val clock: Clock,
) {
    @GetMapping
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun list(): List<AgentResponse> {
        val now = clock.instant()
        val byId = statuses.list().associateBy(AgentStatus::agentId)
        val revisions = desired.all()
        val totals = runtime.all()

        return agents.list().map { agent ->
            val status = byId[agent.id]
            val revision = revisions[agent.id]
            val snapshot = totals[agent.id]
            AgentResponse(
                id = agent.id,
                kind = agent.kind.databaseValue(),
                name = agent.name,
                // Онлайн считается при чтении: хранить его флагом означало бы
                // держать фоновую job только ради его устаревания.
                online = status?.online(now) ?: false,
                appliedRevision = status?.appliedRevision ?: 0,
                desiredRevision = revision?.revision ?: 0,
                appliedSha256 = status?.appliedSha256,
                desiredSha256 = revision?.configSha256,
                agentVersion = status?.agentVersion,
                coreVersion = status?.details?.get("coreVersion") as? String,
                applyError = status?.applyError?.takeIf(String::isNotBlank),
                bootId = status?.bootId,
                lastReportAt = status?.lastReportAt,
                activeSessions = snapshot?.activeSessions ?: 0,
                activeLanes = snapshot?.activeLanes ?: 0,
                // Агент отчитывается, а снимка нет или он протух — ядро молчит,
                // хотя сам агент жив. Контракт ноды такого поля не несёт,
                // поэтому это единственный доступный признак.
                coreReporting = snapshot?.observedAt?.let { it.isAfter(now.minus(AgentStatus.OFFLINE_AFTER)) } ?: false,
                coreSnapshotAt = snapshot?.observedAt,
            )
        }
    }
}

data class AgentResponse(
    val id: String,
    val kind: String,
    val name: String,
    val online: Boolean,
    val appliedRevision: Long,
    /** Расхождение с [appliedRevision] и есть «синхронизируется». */
    val desiredRevision: Long,
    val appliedSha256: String?,
    val desiredSha256: String?,
    val agentVersion: String?,
    val coreVersion: String?,
    val applyError: String?,
    /** Мелькающий boot_id означает, что за ноду борются два агента. */
    val bootId: String?,
    val lastReportAt: Instant?,
    val activeSessions: Int,
    val activeLanes: Int,
    val coreReporting: Boolean,
    val coreSnapshotAt: Instant?,
)
