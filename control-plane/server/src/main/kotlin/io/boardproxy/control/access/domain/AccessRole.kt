package io.boardproxy.control.access.domain

enum class AccessRole {
    SUBSCRIBER,
    VIEWER,
    OPERATOR,
    ADMIN;

    fun authorities(): Set<String> = when (this) {
        SUBSCRIBER -> setOf("ROLE_SUBSCRIBER")
        VIEWER -> setOf("ROLE_VIEWER")
        OPERATOR -> setOf("ROLE_VIEWER", "ROLE_OPERATOR")
        ADMIN -> setOf("ROLE_VIEWER", "ROLE_OPERATOR", "ROLE_ADMIN")
    }
}

data class AccessPrincipal(val name: String, val role: AccessRole)
