package io.boardproxy.control.provisioning.domain.model

class DomainViolation(message: String) : IllegalArgumentException(message)

private val idPattern = Regex("^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$")

internal fun validId(value: String) = idPattern.matches(value)

internal fun requireDomain(condition: Boolean, message: String) {
    if (!condition) throw DomainViolation(message)
}
