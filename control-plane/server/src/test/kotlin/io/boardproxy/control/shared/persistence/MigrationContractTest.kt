package io.boardproxy.control.shared.persistence

import kotlin.test.Test
import kotlin.test.assertContains
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class MigrationContractTest {
    @Test
    fun `initial migration contains durable control and runtime boundaries`() {
        val sql = requireNotNull(javaClass.getResource("/db/migration/V1__control_plane.sql")).readText()
        listOf(
            "CREATE TABLE desired_config_revisions",
            "CREATE TABLE runtime_events",
            "CREATE TABLE node_runtime_projection",
            "CREATE TABLE traffic_batches",
            "CREATE TABLE interface_traffic_deltas",
            "CREATE TABLE user_traffic_deltas",
            "CREATE TABLE audit_events",
            "CREATE TABLE outbox_events",
        ).forEach { assertContains(sql, it) }
        assertContains(sql, "private_key_ciphertext")
        assertContains(sql, "server_key_ciphertext")
        assertContains(sql, "config_ciphertext")
        assertFalse(sql.contains("private_key text", ignoreCase = true))
        assertFalse(sql.contains("server_private_key", ignoreCase = true))
        assertFalse(sql.contains("config_toml", ignoreCase = true))
    }

    @Test
    fun `runtime projection migration stores replay and session completeness state`() {
        val migration = requireNotNull(javaClass.getResource("/db/migration/V3__runtime_projection.sql")).readText()
        listOf(
            "PRIMARY KEY (node_id, event_id)",
            "ADD COLUMN runtime_revision",
            "ADD COLUMN captured_at",
            "ADD COLUMN sessions",
            "ADD COLUMN session_details_complete",
        ).forEach { expected -> assertTrue(migration.contains(expected), "missing: $expected") }
    }

    @Test
    fun `access migration stores hashes and never plaintext tokens`() {
        val sql = requireNotNull(javaClass.getResource("/db/migration/V2__access_tokens.sql")).readText()
        assertContains(sql, "CREATE TABLE api_tokens")
        assertContains(sql, "token_hash")
        assertFalse(sql.contains("token_secret", ignoreCase = true))
        assertFalse(sql.contains("plaintext", ignoreCase = true))
    }

    @Test
    fun `catalog history migration encrypts snapshots and allows rollback hashes`() {
        val sql = requireNotNull(javaClass.getResource("/db/migration/V4__catalog_history.sql")).readText()
        assertContains(sql, "CREATE TABLE catalog_snapshots")
        assertContains(sql, "payload_ciphertext")
        assertContains(sql, "DROP CONSTRAINT desired_config_revisions_node_id_config_sha256_key")
        assertFalse(sql.contains("payload json", ignoreCase = true))
    }

    @Test
    fun `telemetry migration keeps traffic kinds separate and persists quota state`() {
        val sql = requireNotNull(javaClass.getResource("/db/migration/V5__traffic_rollups_and_quotas.sql")).readText()
        listOf("CREATE TABLE traffic_hourly_rollups", "traffic_kind", "CREATE TABLE user_traffic_quotas", "CREATE TABLE user_traffic_quota_state")
            .forEach { assertContains(sql, it) }
    }

    @Test
    fun `ha migration provides certificate revocation fencing and durable retries`() {
        val sql = requireNotNull(javaClass.getResource("/db/migration/V6__security_and_ha.sql")).readText()
        listOf("fingerprint_sha256", "revoked_reason", "CREATE TABLE node_session_leases", "fencing_token", "next_attempt_at", "dead_lettered_at")
            .forEach { assertContains(sql, it) }
    }

    @Test
    fun `runtime replay migration persists authoritative snapshot separately from protobuf payload`() {
        val sql = requireNotNull(javaClass.getResource("/db/migration/V7__runtime_replay.sql")).readText()
        assertContains(sql, "ADD COLUMN snapshot jsonb")
        assertContains(sql, "WHERE snapshot IS NOT NULL")
    }

    @Test
    fun `subscription migration stores only token hashes and supports multiple ordered keys`() {
        val sql = requireNotNull(javaClass.getResource("/db/migration/V8__subscriptions.sql")).readText()
        assertContains(sql, "CREATE TABLE subscriptions")
        assertContains(sql, "token_hash")
        assertContains(sql, "recovery_public_key")
        assertContains(sql, "CREATE TABLE subscription_keys")
        assertContains(sql, "UNIQUE (subscription_id, node_id, user_id)")
        kotlin.test.assertFalse(sql.contains("client_private_key"))
    }

    @Test
    fun `panel authentication migration stores password and session hashes only`() {
        val sql = requireNotNull(javaClass.getResource("/db/migration/V9__panel_authentication.sql")).readText()
        assertContains(sql, "CREATE TABLE panel_administrators")
        assertContains(sql, "password_hash")
        assertContains(sql, "CREATE TABLE panel_sessions")
        assertContains(sql, "token_hash")
        assertFalse(sql.contains("password text", ignoreCase = true))
        assertFalse(sql.contains("token_secret", ignoreCase = true))
    }
}
