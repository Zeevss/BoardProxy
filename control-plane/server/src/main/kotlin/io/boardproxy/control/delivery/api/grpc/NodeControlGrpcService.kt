package io.boardproxy.control.delivery.api.grpc

import bproxy.node.v1.Node
import bproxy.node.v1.NodeControlServiceGrpcKt
import com.google.protobuf.Timestamp
import io.boardproxy.control.delivery.application.DesiredRevisionSignals
import io.boardproxy.control.delivery.application.NodeSession
import io.boardproxy.control.delivery.application.NodeStatusRepository
import io.boardproxy.control.delivery.application.NodeStatusNotifier
import io.boardproxy.control.delivery.application.NodeSessionLease
import io.boardproxy.control.delivery.application.NodeSessionLeaseRepository
import io.boardproxy.control.delivery.domain.AppliedState
import io.boardproxy.control.delivery.domain.ApplyOutcome
import io.boardproxy.control.delivery.domain.Heartbeat
import io.boardproxy.control.delivery.domain.NodeHello
import io.boardproxy.control.fleet.application.EnrollmentService
import io.boardproxy.control.fleet.application.InvalidEnrollmentToken
import io.boardproxy.control.provisioning.application.ConfigRevisionRepository
import io.boardproxy.control.runtime.application.RuntimeEventIngestion
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.telemetry.application.InterfaceDelta
import io.boardproxy.control.telemetry.application.TrafficBatch
import io.boardproxy.control.telemetry.application.TrafficIngestion
import io.boardproxy.control.telemetry.application.UserDelta
import io.boardproxy.control.shared.config.ControlInstanceId
import io.grpc.Status
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.channelFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import org.springframework.stereotype.Service
import java.time.Clock
import java.time.Instant
import java.time.Duration
import java.util.UUID

@Service
class NodeControlGrpcService(
    private val enrollment: EnrollmentService,
    private val revisions: ConfigRevisionRepository,
    private val statuses: NodeStatusRepository,
    private val statusNotifier: NodeStatusNotifier,
    private val signals: DesiredRevisionSignals,
    private val traffic: TrafficIngestion,
    private val runtimeEvents: RuntimeEventIngestion,
    private val leases: NodeSessionLeaseRepository,
    private val instanceId: ControlInstanceId,
    private val clock: Clock,
) : NodeControlServiceGrpcKt.NodeControlServiceCoroutineImplBase() {
    override suspend fun enroll(request: Node.EnrollRequest): Node.EnrollResponse = grpcCall {
        val issued = withContext(Dispatchers.IO) {
            enrollment.enroll(request.nodeId, request.token, request.csrPem.toByteArray())
        }
        Node.EnrollResponse.newBuilder()
            .setCertificatePem(com.google.protobuf.ByteString.copyFrom(issued.certificatePem))
            .setCaCertificatePem(com.google.protobuf.ByteString.copyFrom(issued.caCertificatePem))
            .setExpiresAt(issued.expiresAt.timestamp())
            .build()
    }

    override suspend fun renew(request: Node.RenewRequest): Node.EnrollResponse = grpcCall {
        val nodeId = authenticatedNodeId()
        val issued = withContext(Dispatchers.IO) {
            enrollment.renew(nodeId, request.csrPem.toByteArray())
        }
        Node.EnrollResponse.newBuilder()
            .setCertificatePem(com.google.protobuf.ByteString.copyFrom(issued.certificatePem))
            .setCaCertificatePem(com.google.protobuf.ByteString.copyFrom(issued.caCertificatePem))
            .setExpiresAt(issued.expiresAt.timestamp())
            .build()
    }

    override fun connect(requests: Flow<Node.NodeEvent>): Flow<Node.HubCommand> = channelFlow {
        val authenticatedNodeId = authenticatedNodeId()
        var session: NodeSession? = null
        var activeLease: NodeSessionLease? = null
        var leaseJob: kotlinx.coroutines.Job? = null
        val sessionLock = Mutex()

        suspend fun sendPending() = sessionLock.withLock {
            val active = session ?: return@withLock
            val desired = withContext(Dispatchers.IO) { active.pendingDesired(clock.instant()) } ?: return@withLock
            send(
                Node.HubCommand.newBuilder().setDesiredState(
                    Node.DesiredState.newBuilder()
                        .setRevision(desired.revision)
                        .setConfigToml(com.google.protobuf.ByteString.copyFrom(desired.configToml))
                        .setConfigSha256(desired.configSha256),
                ).build(),
            )
        }

        val signalJob = launch {
            signals.changes(authenticatedNodeId).collect { sendPending() }
        }
        val reconcileJob = launch {
            while (isActive) {
                delay(RECONCILE_INTERVAL_MILLIS)
                sendPending()
            }
        }

        try {
            var first = true
            requests.collect { event ->
                if (first) {
                    first = false
                    if (!event.hasHello()) invalid("hello must be the first node event")
                    val hello = event.hello
                    if (hello.nodeId != authenticatedNodeId) denied("hello node_id does not match client certificate")
                    activeLease = withContext(Dispatchers.IO) {
                        leases.acquire(
                            authenticatedNodeId, instanceId.value, UUID.randomUUID().toString(),
                            clock.instant(), LEASE_TTL,
                        )
                    } ?: throw Status.ALREADY_EXISTS
                        .withDescription("node already has an active control session")
                        .asRuntimeException()
                    leaseJob = launch {
                        while (isActive) {
                            delay(LEASE_RENEW_INTERVAL.toMillis())
                            val current = activeLease ?: return@launch
                            activeLease = withContext(Dispatchers.IO) {
                                leases.renew(current, clock.instant(), LEASE_TTL)
                            } ?: throw Status.ABORTED
                                .withDescription("node control-session lease was lost")
                                .asRuntimeException()
                        }
                    }
                    session = NodeSession(
                        authenticatedNodeId, revisions, statuses,
                        statusNotifier, AppliedState(hello.appliedRevision, hello.configSha256),
                        requireNotNull(activeLease).fencingToken,
                    ).also { active ->
                        withContext(Dispatchers.IO) {
                            active.connected(
                                NodeHello(
                                    hello.bootId, hello.agentVersion, hello.coreVersion,
                                    hello.appliedRevision, hello.configSha256,
                                ),
                                clock.instant(),
                            )
                        }
                    }
                    sendPending()
                } else {
                    val active = session ?: invalid("node session is not initialized")
                    handleEvent(authenticatedNodeId, active, event) { command -> send(command) }
                }
            }
            if (first) invalid("hello must be the first node event")
        } finally {
            signalJob.cancelAndJoin()
            reconcileJob.cancelAndJoin()
            leaseJob?.cancelAndJoin()
            session?.let { active ->
                runCatching { withContext(Dispatchers.IO) { active.disconnected(clock.instant()) } }
            }
            activeLease?.let { lease ->
                runCatching { withContext(Dispatchers.IO) { leases.release(lease) } }
            }
        }
    }

    private suspend fun handleEvent(
        nodeId: String,
        session: NodeSession,
        event: Node.NodeEvent,
        respond: suspend (Node.HubCommand) -> Unit,
    ) {
        when (event.payloadCase) {
            Node.NodeEvent.PayloadCase.APPLY_RESULT -> {
                val result = event.applyResult
                val appliedAt = if (result.hasAppliedAt()) result.appliedAt.instant() else clock.instant()
                withContext(Dispatchers.IO) {
                    session.recordApply(
                        ApplyOutcome(
                            result.desiredRevision, result.runtimeRevision, result.configSha256,
                            result.error, appliedAt,
                        ),
                        clock.instant(),
                    )
                }
            }
            Node.NodeEvent.PayloadCase.HEARTBEAT -> {
                val heartbeat = event.heartbeat
                withContext(Dispatchers.IO) {
                    session.recordHeartbeat(
                        Heartbeat(
                            if (heartbeat.hasSampledAt()) heartbeat.sampledAt.instant() else clock.instant(),
                            heartbeat.coreRunning, heartbeat.coreReady, heartbeat.appliedRevision, heartbeat.error,
                        ),
                    )
                }
            }
            Node.NodeEvent.PayloadCase.INTERFACE_TRAFFIC -> {
                val batch = event.interfaceTraffic
                validateBatch(batch.batchId, batch.hasIntervalStart(), batch.hasIntervalEnd())
                val value = TrafficBatch(
                    nodeId, batch.batchId, batch.intervalStart.instant(), batch.intervalEnd.instant(),
                    batch.interfacesList.map {
                        InterfaceDelta(
                            it.`interface`, it.rxBytes, it.txBytes, it.rxPackets, it.txPackets,
                            it.rxErrors, it.txErrors, it.rxDropped, it.txDropped,
                        )
                    },
                    batch.toByteArray(),
                )
                withContext(Dispatchers.IO) { traffic.storeInterface(value) }
                respond(trafficAck(batch.batchId))
            }
            Node.NodeEvent.PayloadCase.USER_TRAFFIC -> {
                val batch = event.userTraffic
                validateBatch(batch.batchId, batch.hasIntervalStart(), batch.hasIntervalEnd())
                val value = TrafficBatch(
                    nodeId, batch.batchId, batch.intervalStart.instant(), batch.intervalEnd.instant(),
                    batch.usersList.map { UserDelta(it.userTag, it.rxBytes, it.txBytes) },
                    batch.toByteArray(),
                )
                withContext(Dispatchers.IO) { traffic.storeUsers(value) }
                respond(trafficAck(batch.batchId))
            }
            Node.NodeEvent.PayloadCase.RUNTIME_EVENTS -> {
                val batch = event.runtimeEvents
                withContext(Dispatchers.IO) {
                    runtimeEvents.store(batch.toDomain(nodeId))
                }
                respond(
                    Node.HubCommand.newBuilder().setRuntimeEventAck(
                        Node.RuntimeEventAck.newBuilder().setBatchId(batch.batchId),
                    ).build(),
                )
            }
            Node.NodeEvent.PayloadCase.HELLO -> invalid("hello can only be the first node event")
            Node.NodeEvent.PayloadCase.PAYLOAD_NOT_SET -> invalid("node event payload is required")
        }
    }

    private fun trafficAck(batchId: String): Node.HubCommand = Node.HubCommand.newBuilder()
        .setTrafficAck(Node.TrafficAck.newBuilder().setBatchId(batchId))
        .build()

    private fun validateBatch(batchId: String, hasStart: Boolean, hasEnd: Boolean) {
        if (batchId.isBlank() || !hasStart || !hasEnd) invalid("traffic batch identity and interval are required")
    }

    private fun authenticatedNodeId(): String = NodeIdentityInterceptor.currentNodeId()
        ?: throw Status.UNAUTHENTICATED.withDescription("valid node client certificate is required").asRuntimeException()

    private suspend fun <T> grpcCall(block: suspend () -> T): T = try {
        block()
    } catch (error: InvalidEnrollmentToken) {
        throw Status.UNAUTHENTICATED.withDescription(error.message).asRuntimeException()
    } catch (error: InvalidRequest) {
        throw Status.INVALID_ARGUMENT.withDescription(error.message).asRuntimeException()
    }

    private fun invalid(message: String): Nothing =
        throw Status.INVALID_ARGUMENT.withDescription(message).asRuntimeException()

    private fun denied(message: String): Nothing =
        throw Status.PERMISSION_DENIED.withDescription(message).asRuntimeException()

    private fun Timestamp.instant(): Instant = Instant.ofEpochSecond(seconds, nanos.toLong())
    private fun Instant.timestamp(): Timestamp = Timestamp.newBuilder().setSeconds(epochSecond).setNanos(nano).build()

    private companion object {
        const val RECONCILE_INTERVAL_MILLIS = 30_000L
        val LEASE_TTL: Duration = Duration.ofSeconds(30)
        val LEASE_RENEW_INTERVAL: Duration = Duration.ofSeconds(10)
    }
}
