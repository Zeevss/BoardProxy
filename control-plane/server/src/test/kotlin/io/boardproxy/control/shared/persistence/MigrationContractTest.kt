package io.boardproxy.control.shared.persistence

import io.boardproxy.control.testing.PostgresSupport
import org.springframework.dao.DataIntegrityViolationException
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

/**
 * Контракт схемы проверяется на живой базе, а не сравнением строк в .sql.
 *
 * Доступ к данным рукописный, поэтому компилятор не поймает расхождение схемы
 * и кода — эту роль берут на себя эти тесты и Testcontainers-тесты репозиториев.
 */
class MigrationContractTest {
    private val jdbc get() = PostgresSupport.jdbc

    @BeforeTest
    fun prepare() {
        kotlin.test.assertTrue(PostgresSupport.dockerAvailable, "тесты схемы требуют Docker")
        PostgresSupport.truncate()
    }

    @Test
    fun `схема содержит ровно ожидаемый набор таблиц`() {
        val actual = jdbc.queryForList(
            "SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename <> 'flyway_schema_history'",
            String::class.java,
        ).toSortedSet()

        val expected = sortedSetOf(
            // агенты
            "agents", "agent_status", "agent_boots", "agent_commands", "agent_reports",
            // владеемое состояние
            "nodes", "boards", "users", "grants",
            // производное
            "node_desired_config", "node_config_snapshots",
            // наблюдаемое
            "node_runtime", "runtime_events",
            // трафик
            "interface_traffic_deltas", "user_traffic_deltas", "traffic_hourly_rollups",
            "user_traffic_lifetime_totals", "user_traffic_quotas", "user_traffic_quota_state",
            // доступ
            "credentials", "panel_administrators",
            // подписки
            "subscriptions", "subscription_service_settings",
            // pki и журналы
            "enrollment_tokens", "node_certificates", "audit_events", "outbox_events",
            "quota_config_changes",
        )

        assertEquals(expected, actual)
    }

    @Test
    fun `секреты не хранятся открытым текстом`() {
        val secretish = jdbc.queryForList(
            """SELECT table_name || '.' || column_name
                 FROM information_schema.columns
                WHERE table_schema = 'public'
                  AND data_type IN ('text', 'character varying', 'character')
                  AND (column_name LIKE '%private%' OR column_name LIKE '%password%'
                       OR column_name LIKE '%secret%' OR column_name LIKE '%token%')
                  AND column_name NOT LIKE '%\_hash'
                  AND column_name NOT LIKE '%\_id'""",
            String::class.java,
        )

        assertTrue(secretish.isEmpty(), "секрет в открытом виде: $secretish")
    }

    @Test
    fun `удаление ноды не удаляет флотового пользователя`() {
        seedNodeWithUser()

        jdbc.update("DELETE FROM agents WHERE id = 'n1'")

        assertEquals(0, count("nodes"), "нода должна уйти")
        assertEquals(0, count("boards"), "борды ноды должны уйти каскадом")
        assertEquals(0, count("grants"), "гранты на борды ноды должны уйти каскадом")
        assertEquals(1, count("users"), "пользователь флотовый и ноду переживает")
    }

    @Test
    fun `грант нельзя выдать на борд чужой ноды`() {
        seedNodeWithUser()
        jdbc.update("INSERT INTO agents(id, kind, name) VALUES ('n2', 'node', 'node two')")
        jdbc.update(
            """INSERT INTO nodes VALUES
               ('n2', 'node two', 'enabled', '{}'::jsonb, '\x00', '\x00', 'k1', 1, now())""",
        )

        assertFailsWith<DataIntegrityViolationException> {
            jdbc.update("INSERT INTO grants VALUES ('u1', 'n2', 'b1')")
        }
    }

    @Test
    fun `отпечаток пользователя уникален во всём флоте`() {
        seedNodeWithUser()

        assertFailsWith<DataIntegrityViolationException> {
            jdbc.update(
                """INSERT INTO users VALUES
                   ('u2', 'other', NULL, NULL, NULL, 'pub2', 'fp1', 'enabled', 2, 4, 1, now())""",
            )
        }
    }

    @Test
    fun `пользователь обязан иметь либо приватный ключ, либо публичный`() {
        assertFailsWith<DataIntegrityViolationException> {
            jdbc.update(
                """INSERT INTO users VALUES
                   ('u3', 'broken', NULL, NULL, NULL, NULL, 'fp3', 'enabled', 2, 4, 1, now())""",
            )
        }
    }

    @Test
    fun `повторный отчёт агента отсекается первичным ключом`() {
        seedNodeWithUser()
        jdbc.update("INSERT INTO agent_reports(agent_id, batch_id) VALUES ('n1', 'batch-1')")

        val inserted = jdbc.update(
            "INSERT INTO agent_reports(agent_id, batch_id) VALUES ('n1', 'batch-1') ON CONFLICT DO NOTHING",
        )

        assertEquals(0, inserted, "дубликат отчёта не должен приниматься повторно")
    }

    private fun seedNodeWithUser() {
        jdbc.update("INSERT INTO agents(id, kind, name) VALUES ('n1', 'node', 'node one')")
        jdbc.update(
            """INSERT INTO nodes VALUES
               ('n1', 'node one', 'enabled', '{}'::jsonb, '\x00', '\x00', 'k1', 1, now())""",
        )
        jdbc.update(
            """INSERT INTO boards VALUES
               ('n1', 'b1', 'board', 'hash1', NULL, NULL, NULL, 'enabled', 4, 1, now())""",
        )
        jdbc.update(
            """INSERT INTO users VALUES
               ('u1', 'user', NULL, NULL, NULL, 'pub', 'fp1', 'enabled', 2, 4, 1, now())""",
        )
        jdbc.update("INSERT INTO grants VALUES ('u1', 'n1', 'b1')")
    }

    private fun count(table: String): Int = jdbc.queryForObject("SELECT count(*) FROM $table", Int::class.java) ?: 0
}
