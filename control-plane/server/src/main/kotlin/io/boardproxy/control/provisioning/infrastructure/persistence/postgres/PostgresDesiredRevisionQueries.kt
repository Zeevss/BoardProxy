package io.boardproxy.control.provisioning.infrastructure.persistence.postgres

import io.boardproxy.control.shared.contracts.DesiredRevision
import io.boardproxy.control.shared.contracts.DesiredRevisionQueries
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate
import org.springframework.stereotype.Repository

/**
 * Ревизии всех нод одним запросом, без расшифровки TOML.
 *
 * Отдельно от [PostgresDesiredConfigRepository] именно поэтому: тот читает
 * конфигурацию целиком и обязан её расшифровать, а списку нод нужны два поля,
 * лежащие открытым текстом.
 */
@Repository
class PostgresDesiredRevisionQueries(
    private val jdbc: NamedParameterJdbcTemplate,
) : DesiredRevisionQueries {

    override fun all(): Map<String, DesiredRevision> = jdbc.query(
        "SELECT node_id, revision, config_sha256 FROM node_desired_config",
        emptyMap<String, Any>(),
    ) { rs, _ ->
        rs.getString("node_id") to DesiredRevision(rs.getLong("revision"), rs.getString("config_sha256"))
    }.toMap()
}
