package io.boardproxy.control.shared.agents

import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController
import java.time.Clock
import java.time.Instant

/**
 * Ноды и сервис подписок одним списком: у них общее понятие состояния, поэтому
 * и экран в панели общий.
 */
@RestController
@RequestMapping("/api/v1/agents")
class AgentDirectoryController(
    private val agents: AgentRegistry,
    private val statuses: AgentStatusRepository,
    private val clock: Clock,
) {
    @GetMapping
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun list(): List<AgentResponse> {
        val now = clock.instant()
        val byId = statuses.list().associateBy(AgentStatus::agentId)
        return agents.list().map { agent ->
            val status = byId[agent.id]
            AgentResponse(
                id = agent.id,
                kind = agent.kind.databaseValue(),
                name = agent.name,
                // Онлайн считается при чтении: хранить его флагом означало бы
                // держать фоновую job только ради его устаревания.
                online = status?.online(now) ?: false,
                appliedRevision = status?.appliedRevision ?: 0,
                agentVersion = status?.agentVersion,
                applyError = status?.applyError?.takeIf(String::isNotBlank),
                bootId = status?.bootId,
                lastReportAt = status?.lastReportAt,
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
    val agentVersion: String?,
    val applyError: String?,
    /** Мелькающий boot_id означает, что за ноду борются два агента. */
    val bootId: String?,
    val lastReportAt: Instant?,
)
