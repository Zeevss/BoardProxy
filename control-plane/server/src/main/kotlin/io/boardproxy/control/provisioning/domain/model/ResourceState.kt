package io.boardproxy.control.provisioning.domain.model

enum class ResourceState {
    ENABLED,
    DISABLED,
    REVOKED;

    val isEnabled: Boolean get() = this == ENABLED
}
