package io.boardproxy.control.provisioning.domain.model

import java.time.Instant
import java.util.Base64
import kotlin.test.Test
import kotlin.test.assertFailsWith

class CatalogTest {
    @Test
    fun `duplicate cryptographic identities are rejected`() {
        assertFailsWith<DomainViolation> {
            validCatalog().let { catalog -> catalog.copy(users = listOf(catalog.users.first(), catalog.users.first().copy(id = "other"))) }
        }
    }

    @Test
    fun `public management endpoint must stay on loopback`() {
        assertFailsWith<DomainViolation> {
            validCatalog().let { catalog ->
                catalog.copy(node = catalog.node.copy(core = catalog.node.core.copy(management = ManagementSettings("0.0.0.0:9090"))))
            }
        }
    }

    private fun validCatalog(): Catalog {
        val now = Instant.EPOCH
        return Catalog(
            node = Node("node", "Node", ResourceState.ENABLED, CoreSettings.defaults(key(1)), 1, now),
            boards = listOf(Board("board", "Board", "hash", state = ResourceState.ENABLED, maxLanes = 2, version = 1, updatedAt = now)),
            users = listOf(User("user", "User", privateKey = key(2), state = ResourceState.ENABLED, maxSessions = 1, maxLanes = 2, version = 1, updatedAt = now)),
            assignment = NodeAssignment("node", listOf("board"), listOf(AssignedUser("user", listOf("board"))), 1, now),
            version = 1,
            updatedAt = now,
        )
    }

    private fun key(value: Byte) = "base64:" + Base64.getEncoder().encodeToString(ByteArray(32) { value })
}
