package io.boardproxy.control.access.infrastructure.config

import io.boardproxy.control.access.application.ApiTokenCommands
import io.boardproxy.control.access.domain.AccessRole
import io.boardproxy.control.shared.contracts.IssuedServiceToken
import io.boardproxy.control.shared.contracts.ServiceTokenIssuer
import org.springframework.stereotype.Component

/**
 * Сводит порт выпуска токенов с реализацией доступа. Живёт в инфраструктуре:
 * знать обе стороны — её работа, а не работа прикладного слоя подписок.
 */
@Component
class ServiceTokenAdapter(private val tokens: ApiTokenCommands) : ServiceTokenIssuer {

    /** Роль и бессрочность — политика доступа, поэтому решаются здесь, а не вызывающим. */
    override fun issueSubscriberToken(name: String, actor: String): IssuedServiceToken {
        val issued = tokens.issue(name, AccessRole.SUBSCRIBER, ttl = null, actor = actor)
        return IssuedServiceToken(issued.token.id, issued.secret)
    }

    override fun revoke(tokenId: String, actor: String) {
        tokens.revokeIfActive(tokenId, actor)
    }
}
