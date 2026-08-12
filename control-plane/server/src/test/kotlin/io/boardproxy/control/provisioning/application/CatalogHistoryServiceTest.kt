package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.Catalog
import io.boardproxy.control.provisioning.domain.model.ConfigRevision
import io.boardproxy.control.testing.TestCatalogs
import java.time.Clock
import java.time.Instant
import java.time.ZoneOffset
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse

class CatalogHistoryServiceTest {
    private val now = Instant.parse("2026-08-12T12:00:00Z")

    @Test
    fun `diff never exposes private keys`() {
        val first = TestCatalogs.catalog()
        val second = TestCatalogs.catalog(version = 2).copy(
            users = listOf(TestCatalogs.catalog(version = 2).users.single().copy(name = "Bob")),
        )
        val snapshots = Snapshots(first, second)
        val commands = Commands(second)
        val service = CatalogHistoryService(commands, snapshots, commands, Clock.fixed(now, ZoneOffset.UTC))

        val diff = service.diff("node-1", 1, 2)

        assertEquals("users.user-1.name", diff.changes.single().path)
        assertFalse(diff.changes.joinToString().contains(TestCatalogs.key(2)))
    }

    @Test
    fun `rollback restores content as a new monotonic version`() {
        val first = TestCatalogs.catalog()
        val current = TestCatalogs.catalog(version = 3).copy(node = TestCatalogs.catalog(version = 3).node.copy(name = "New"))
        val snapshots = Snapshots(first, current)
        val commands = Commands(current)
        val service = CatalogHistoryService(commands, snapshots, commands, Clock.fixed(now, ZoneOffset.UTC))

        val result = service.rollback("node-1", 1, 3, "operator")

        assertEquals("Primary", result.catalog.node.name)
        assertEquals(4, result.catalog.version)
        assertEquals(4, result.catalog.node.version)
        assertEquals("catalog.rolled-back", commands.cause)
    }

    private class Snapshots(vararg catalogs: Catalog) : CatalogSnapshotRepository {
        private val values = catalogs.associateBy { it.version }.toMutableMap()
        override fun save(catalog: Catalog) { values.putIfAbsent(catalog.version, catalog) }
        override fun find(nodeId: String, catalogVersion: Long): Catalog? = values[catalogVersion]
        override fun list(nodeId: String, offset: Int, limit: Int) = values.values.sortedByDescending { it.version }
            .drop(offset).take(limit).map { CatalogSnapshotMetadata(nodeId, it.version, it.updatedAt) }
        override fun count(nodeId: String): Long = values.size.toLong()
    }

    private class Commands(var catalog: Catalog) : CatalogQueries, CatalogCommands {
        var cause: String? = null
        override fun get(nodeId: String): Catalog = catalog
        override fun create(catalog: Catalog, actor: String): CatalogMutationResult = error("not used")
        override fun replace(
            catalog: Catalog,
            expectedVersion: Long,
            actor: String,
            cause: String,
        ): CatalogMutationResult {
            this.catalog = catalog
            this.cause = cause
            return CatalogMutationResult(
                catalog,
                ConfigRevision(catalog.node.id, 4, 3, catalog.version, byteArrayOf(), "hash", cause, catalog.updatedAt),
                true,
            )
        }
    }
}
