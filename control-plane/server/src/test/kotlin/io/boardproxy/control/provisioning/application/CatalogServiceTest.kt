package io.boardproxy.control.provisioning.application

import io.boardproxy.control.audit.application.AuditRepository
import io.boardproxy.control.audit.domain.AuditEvent
import io.boardproxy.control.provisioning.domain.model.Catalog
import io.boardproxy.control.provisioning.domain.model.ConfigRevision
import io.boardproxy.control.shared.events.OutboxEvent
import io.boardproxy.control.shared.events.OutboxRepository
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.testing.TestCatalogs
import java.security.MessageDigest
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertTrue

class CatalogServiceTest {
    private val now = Instant.parse("2026-02-03T04:05:06Z")

    @Test
    fun `create persists catalog revision audit and outbox in one transaction`() {
        val fixture = Fixture()

        val result = fixture.service.create(TestCatalogs.catalog(), "operator@example.com")

        assertEquals(1, fixture.transactions)
        assertNotNull(fixture.catalogs.value)
        assertEquals(1, fixture.revisions.values.size)
        assertEquals("catalog.created", fixture.audit.single().action)
        assertEquals("desired-state.changed", fixture.outbox.single().type)
        assertEquals(1, result.desiredRevision.revision)
        assertTrue(result.configChanged)
    }

    @Test
    fun `metadata-only replacement keeps revision and does not emit desired-state event`() {
        val fixture = Fixture(initial = TestCatalogs.catalog())
        fixture.revisions.append("node-1", 1, "stable".toByteArray(), "seed", now)
        fixture.compilerBytes = "stable".toByteArray()
        val replacement = TestCatalogs.catalog(version = 2).copy(
            node = TestCatalogs.catalog(version = 2).node.copy(name = "Renamed"),
        )

        val result = fixture.service.replace(replacement, expectedVersion = 1, actor = "operator")

        assertEquals(1, result.desiredRevision.revision)
        assertFalse(result.configChanged)
        assertTrue(fixture.outbox.isEmpty())
        assertEquals("catalog.replaced", fixture.audit.single().action)
    }

    private inner class Fixture(initial: Catalog? = null) {
        var transactions = 0
        var compilerBytes = "generated-config".toByteArray()
        val catalogs = InMemoryCatalogRepository(initial)
        val revisions = InMemoryRevisionRepository()
        val snapshots = InMemorySnapshotRepository()
        val audit = mutableListOf<AuditEvent>()
        val outbox = mutableListOf<OutboxEvent>()
        private var ids = 0
        val service = CatalogService(
            catalogs = catalogs,
            snapshots = snapshots,
            compiler = CoreConfigCompiler { compilerBytes },
            revisions = revisions,
            audit = AuditRepository(audit::add),
            outbox = OutboxRepository(outbox::add),
            transactions = object : TransactionRunner {
                override fun <T> required(block: () -> T): T {
                    transactions++
                    return block()
                }
            },
            clock = Clock.fixed(now, ZoneOffset.UTC),
            nextId = { "event-${++ids}" },
        )
    }

    private class InMemoryCatalogRepository(var value: Catalog?) : CatalogRepository {
        override fun find(nodeId: String): Catalog? = value?.takeIf { it.node.id == nodeId }
        override fun search(query: String?, offset: Int, limit: Int): List<Catalog> = listOfNotNull(value).drop(offset).take(limit)
        override fun count(query: String?): Long = if (value == null) 0 else 1

        override fun create(catalog: Catalog) {
            value = catalog
        }

        override fun replace(catalog: Catalog, expectedVersion: Long): Boolean {
            if (value?.version != expectedVersion) return false
            value = catalog
            return true
        }
    }

    private class InMemorySnapshotRepository : CatalogSnapshotRepository {
        val values = mutableMapOf<Pair<String, Long>, Catalog>()
        override fun save(catalog: Catalog) {
            values.putIfAbsent(catalog.node.id to catalog.version, catalog)
        }
        override fun find(nodeId: String, catalogVersion: Long): Catalog? = values[nodeId to catalogVersion]
        override fun list(nodeId: String, offset: Int, limit: Int): List<CatalogSnapshotMetadata> = values.values
            .filter { it.node.id == nodeId }
            .sortedByDescending { it.version }
            .drop(offset)
            .take(limit)
            .map { CatalogSnapshotMetadata(nodeId, it.version, it.updatedAt) }
        override fun count(nodeId: String): Long = values.values.count { it.node.id == nodeId }.toLong()
    }

    private class InMemoryRevisionRepository : ConfigRevisionRepository {
        val values = mutableListOf<ConfigRevision>()

        override fun append(
            nodeId: String,
            catalogVersion: Long,
            configToml: ByteArray,
            cause: String,
            createdAt: Instant,
        ): ConfigRevision {
            val hash = MessageDigest.getInstance("SHA-256").digest(configToml).joinToString("") { "%02x".format(it) }
            values.lastOrNull()?.takeIf { it.nodeId == nodeId && it.configSha256 == hash }?.let { return it }
            return ConfigRevision(
                nodeId, values.size.toLong() + 1, values.lastOrNull()?.revision ?: 0,
                catalogVersion, configToml, hash, cause, createdAt,
            ).also(values::add)
        }

        override fun latest(nodeId: String): ConfigRevision? = values.lastOrNull { it.nodeId == nodeId }
    }
}
