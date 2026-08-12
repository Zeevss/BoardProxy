package io.boardproxy.control.delivery.api.grpc

import bproxy.node.v1.Node
import com.google.protobuf.Timestamp
import io.boardproxy.control.runtime.domain.RuntimeEventPayload
import io.grpc.Status
import io.grpc.StatusRuntimeException
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertIs

class RuntimeEventGrpcMapperTest {
    @Test
    fun `maps reset snapshot and normal events into domain`() {
        val batch = Node.RuntimeEventBatch.newBuilder()
            .setBatchId("batch-1")
            .addEvents(
                baseEvent("reset", 0).setStreamReset(
                    Node.EventStreamReset.newBuilder()
                        .setReason("event_gap")
                        .setOldestAvailableSequence(5)
                        .setLatestSequence(9),
                ),
            )
            .setSnapshot(
                Node.RuntimeSnapshot.newBuilder()
                    .setCoreBootId("boot-1")
                    .setLatestSequence(9)
                    .setRuntimeRevision(3)
                    .setCapturedAt(timestamp())
                    .addUsers(
                        Node.RuntimeUserSnapshot.newBuilder()
                            .setUserTag("alice")
                            .setActiveSessions(1)
                            .setRxBytes(10)
                            .setTxBytes(20),
                    )
                    .addBoards(
                        Node.RuntimeBoardSnapshot.newBuilder()
                            .setBoardTag("main")
                            .setState("ready"),
                    ),
            )
            .build()

        val mapped = batch.toDomain("node-1")

        assertEquals("node-1", mapped.nodeId)
        assertIs<RuntimeEventPayload.StreamReset>(mapped.events.single().payload)
        assertEquals(9, mapped.snapshot?.latestSequence)
        assertEquals("alice", mapped.snapshot?.users?.single()?.userTag)
    }

    @Test
    fun `rejects non-reset event with zero sequence`() {
        val batch = Node.RuntimeEventBatch.newBuilder()
            .setBatchId("batch-1")
            .addEvents(
                baseEvent("event-1", 0).setClientSessionOpened(
                    Node.ClientSessionOpened.newBuilder()
                        .setUserTag("alice")
                        .setBoardTag("main")
                        .setBundleId("bundle-1"),
                ),
            )
            .build()

        val error = assertFailsWith<StatusRuntimeException> { batch.toDomain("node-1") }

        assertEquals(Status.Code.INVALID_ARGUMENT, error.status.code)
    }

    @Test
    fun `rejects duplicate resources in snapshot`() {
        val snapshot = Node.RuntimeSnapshot.newBuilder()
            .setCoreBootId("boot-1")
            .setCapturedAt(timestamp())
            .addUsers(Node.RuntimeUserSnapshot.newBuilder().setUserTag("alice"))
            .addUsers(Node.RuntimeUserSnapshot.newBuilder().setUserTag("alice"))
        val batch = Node.RuntimeEventBatch.newBuilder().setBatchId("batch-1").setSnapshot(snapshot).build()

        val error = assertFailsWith<StatusRuntimeException> { batch.toDomain("node-1") }

        assertEquals(Status.Code.INVALID_ARGUMENT, error.status.code)
    }

    private fun baseEvent(eventId: String, sequence: Long) = Node.CoreRuntimeEvent.newBuilder()
        .setEventId(eventId)
        .setCoreBootId("boot-1")
        .setSequence(sequence)
        .setOccurredAt(timestamp())
        .setRuntimeRevision(3)

    private fun timestamp() = Timestamp.newBuilder().setSeconds(1_786_528_800).build()
}
