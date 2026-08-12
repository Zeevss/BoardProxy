package io.boardproxy.control.testing

import io.boardproxy.control.provisioning.domain.model.AssignedUser
import io.boardproxy.control.provisioning.domain.model.Board
import io.boardproxy.control.provisioning.domain.model.Catalog
import io.boardproxy.control.provisioning.domain.model.CoreSettings
import io.boardproxy.control.provisioning.domain.model.Node
import io.boardproxy.control.provisioning.domain.model.NodeAssignment
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.provisioning.domain.model.User
import java.time.Instant
import java.util.Base64

object TestCatalogs {
    fun catalog(version: Long = 1, now: Instant = Instant.parse("2026-01-01T00:00:00Z")): Catalog = Catalog(
        node = Node("node-1", "Primary", ResourceState.ENABLED, CoreSettings.defaults(key(1)), version, now),
        boards = listOf(
            Board(
                id = "board-1", name = "Main", hash = "board-hash",
                state = ResourceState.ENABLED, maxLanes = 4, version = version, updatedAt = now,
            ),
        ),
        users = listOf(
            User(
                id = "user-1", name = "Alice", privateKey = key(2), state = ResourceState.ENABLED,
                maxSessions = 2, maxLanes = 3, version = version, updatedAt = now,
            ),
        ),
        assignment = NodeAssignment(
            nodeId = "node-1", boardIds = listOf("board-1"),
            users = listOf(AssignedUser("user-1", listOf("board-1"))),
            version = version, updatedAt = now,
        ),
        version = version,
        updatedAt = now,
    )

    fun key(value: Byte): String = "base64:" + Base64.getEncoder().encodeToString(ByteArray(32) { value })
}
