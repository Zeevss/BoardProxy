package io.boardproxy.control.shared.audit

import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.RestController

/**
 * История действий для ленты активности.
 *
 * Дополняет SSE, а не заменяет его: поток отдаёт то, что происходит сейчас,
 * журнал — то, что панель пропустила, пока её никто не смотрел.
 */
@RestController
@RequestMapping("/api/v1/audit")
class AuditController(private val queries: AuditQueries) {

    @GetMapping
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun list(
        @RequestParam(required = false) nodeId: String?,
        @RequestParam(defaultValue = "0") offset: Int,
        @RequestParam(defaultValue = "50") limit: Int,
    ): AuditPage = queries.list(nodeId?.takeIf(String::isNotBlank), offset.coerceAtLeast(0), limit.coerceIn(1, MAXIMUM_PAGE))

    private companion object {
        const val MAXIMUM_PAGE = 200
    }
}
