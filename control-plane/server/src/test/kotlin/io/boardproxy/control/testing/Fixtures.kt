package io.boardproxy.control.testing

import io.boardproxy.control.provisioning.domain.model.Board
import io.boardproxy.control.provisioning.domain.model.CoreSettings
import io.boardproxy.control.provisioning.domain.model.Node
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.provisioning.domain.model.User
import java.time.Instant
import java.util.Base64

val TEST_TIME: Instant = Instant.parse("2026-08-18T12:00:00Z")

/** Детерминированный ключ: тесты сравнивают значения, а не проверяют случайность. */
fun testKey(value: Byte): String = "base64:" + Base64.getEncoder().encodeToString(ByteArray(32) { value })

fun testNode(
    id: String = "node-1",
    name: String = "Node One",
    state: ResourceState = ResourceState.ENABLED,
    version: Long = 1,
) = Node(id, name, state, CoreSettings.defaults(testKey(1)), version, TEST_TIME)

fun testBoard(
    nodeId: String = "node-1",
    id: String = "board-1",
    hash: String = "hash-1",
    state: ResourceState = ResourceState.ENABLED,
    version: Long = 1,
) = Board(nodeId, id, "Board $id", hash, state = state, maxLanes = 4, version = version, updatedAt = TEST_TIME)

fun testUser(
    id: String = "user-1",
    keyByte: Byte = 2,
    state: ResourceState = ResourceState.ENABLED,
    version: Long = 1,
) = User(
    id = id, name = "User $id", privateKey = testKey(keyByte), state = state,
    maxSessions = 2, maxLanes = 4, version = version, updatedAt = TEST_TIME,
)
