package io.boardproxy.control.delivery.api.grpc

import bproxy.node.v1.Node
import bproxy.node.v1.NodeControlServiceGrpc
import com.google.protobuf.ByteString
import com.google.protobuf.Timestamp
import io.boardproxy.control.delivery.application.DesiredRevisionSignals
import io.boardproxy.control.delivery.application.InterfaceTrafficInput
import io.boardproxy.control.delivery.application.NodeReport
import io.boardproxy.control.delivery.application.NodeReportService
import io.boardproxy.control.delivery.application.RuntimeBoardInput
import io.boardproxy.control.delivery.application.RuntimeEventInput
import io.boardproxy.control.delivery.application.RuntimeSnapshotInput
import io.boardproxy.control.delivery.application.RuntimeUserInput
import io.boardproxy.control.delivery.application.UserTrafficInput
import io.boardproxy.control.fleet.application.EnrollmentService
import io.boardproxy.control.provisioning.application.DesiredConfigRepository
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.errors.ResourceNotFound
import io.grpc.Status
import io.grpc.StatusRuntimeException
import io.grpc.stub.ServerCallStreamObserver
import io.grpc.stub.StreamObserver
import org.slf4j.LoggerFactory
import org.springframework.stereotype.Component
import java.time.Clock
import java.time.Duration
import java.time.Instant

/**
 * Контракт ноды: хаб только сообщает номер ревизии, конфигурацию нода забирает
 * сама и сама же повторяет при ошибке.
 *
 * Обработчики блокирующие и работают на виртуальных потоках. Прежняя реализация
 * держала двунаправленный поток на корутинах и хранила состояние сессии —
 * отсюда росли и лиз с fencing-токеном, и блокирующий JDBC под @Synchronized.
 * Здесь состояния сессии нет вовсе.
 */
@Component
class NodeControlGrpcService(
    private val enrollment: EnrollmentService,
    private val configs: DesiredConfigRepository,
    private val reports: NodeReportService,
    private val signals: DesiredRevisionSignals,
    private val clock: Clock,
) : NodeControlServiceGrpc.NodeControlServiceImplBase() {

    override fun enroll(request: Node.EnrollRequest, observer: StreamObserver<Node.EnrollResponse>) = respond(observer) {
        val issued = enrollment.enroll(request.nodeId, request.token, request.csrPem.toByteArray())
        Node.EnrollResponse.newBuilder()
            .setCertificatePem(ByteString.copyFrom(issued.certificatePem))
            .setCaCertificatePem(ByteString.copyFrom(issued.caCertificatePem))
            .setExpiresAt(issued.expiresAt.timestamp())
            .build()
    }

    override fun renew(request: Node.RenewRequest, observer: StreamObserver<Node.EnrollResponse>) = respond(observer) {
        val issued = enrollment.renew(authenticatedNodeId(), request.csrPem.toByteArray())
        Node.EnrollResponse.newBuilder()
            .setCertificatePem(ByteString.copyFrom(issued.certificatePem))
            .setCaCertificatePem(ByteString.copyFrom(issued.caCertificatePem))
            .setExpiresAt(issued.expiresAt.timestamp())
            .build()
    }

    /**
     * Поток несёт только номера ревизий. Периодический повтор нужен на случай
     * потерянного уведомления: без него нода осталась бы со старой
     * конфигурацией до следующей чужой правки.
     */
    override fun watch(request: Node.WatchRequest, observer: StreamObserver<Node.ConfigNotice>) {
        val nodeId = runCatching { authenticatedNodeId() }.getOrElse { return observer.onError(it) }
        val stream = observer as ServerCallStreamObserver<Node.ConfigNotice>

        signals.subscribe(nodeId).use { subscription ->
            while (!stream.isCancelled) {
                runCatching { configs.find(nodeId) }
                    .onSuccess { config ->
                        if (config != null) {
                            stream.onNext(
                                Node.ConfigNotice.newBuilder()
                                    .setRevision(config.revision)
                                    .setConfigSha256(config.configSha256)
                                    .build(),
                            )
                        }
                    }
                    .onFailure { error -> log.warn("failed to read desired config for node {}", nodeId, error) }

                subscription.await(HEARTBEAT)
            }
        }
        if (!stream.isCancelled) observer.onCompleted()
    }

    override fun fetchConfig(
        request: Node.FetchConfigRequest,
        observer: StreamObserver<Node.ConfigDocument>,
    ) = respond(observer) {
        val nodeId = authenticatedNodeId()
        val config = configs.find(nodeId)
            ?: throw StatusRuntimeException(Status.NOT_FOUND.withDescription("node $nodeId has no configuration"))
        Node.ConfigDocument.newBuilder()
            .setRevision(config.revision)
            .setConfigSha256(config.configSha256)
            .setConfigToml(ByteString.copyFrom(config.configToml))
            .build()
    }

    override fun report(request: Node.ReportRequest, observer: StreamObserver<Node.ReportResponse>) = respond(observer) {
        val nodeId = authenticatedNodeId()
        val commands = reports.accept(request.toReport(nodeId))
        Node.ReportResponse.newBuilder()
            .addAllCommands(
                commands.map { command ->
                    Node.AgentCommand.newBuilder().setNonce(command.nonce).setKind(command.kind).build()
                },
            )
            .build()
    }

    private fun <T> respond(observer: StreamObserver<T>, block: () -> T) {
        try {
            observer.onNext(block())
            observer.onCompleted()
        } catch (error: StatusRuntimeException) {
            observer.onError(error)
        } catch (error: ResourceNotFound) {
            observer.onError(Status.NOT_FOUND.withDescription(error.message).asRuntimeException())
        } catch (error: InvalidRequest) {
            observer.onError(Status.INVALID_ARGUMENT.withDescription(error.message).asRuntimeException())
        } catch (error: ResourceConflict) {
            observer.onError(Status.ABORTED.withDescription(error.message).asRuntimeException())
        } catch (error: Exception) {
            log.warn("node control call failed", error)
            observer.onError(Status.INTERNAL.withDescription("internal error").asRuntimeException())
        }
    }

    private fun Node.ReportRequest.toReport(nodeId: String) = NodeReport(
        nodeId = nodeId,
        bootId = bootId,
        seq = seq,
        batchId = batchId,
        appliedRevision = health.appliedRevision,
        appliedSha256 = health.appliedSha256,
        applyError = health.applyError,
        coreVersion = health.coreVersion,
        agentVersion = health.agentVersion,
        uptimeSeconds = health.uptimeSeconds,
        observedAt = health.observedAt.instant(clock),
        runtime = if (hasRuntime()) {
            RuntimeSnapshotInput(
                coreBootId = runtime.coreBootId,
                capturedAt = runtime.capturedAt.instant(clock),
                users = runtime.usersList.map {
                    RuntimeUserInput(it.userId, it.activeSessions, it.activeLanes, it.lastSeenAt.instantOrNull())
                },
                boards = runtime.boardsList.map {
                    RuntimeBoardInput(it.boardId, it.state, it.activeLanes, it.lastError)
                },
            )
        } else {
            null
        },
        interfaceTraffic = interfaceTrafficList.map {
            InterfaceTrafficInput(
                it.getInterface(), it.rxBytes.toLong(), it.txBytes.toLong(), it.rxPackets.toLong(),
                it.txPackets.toLong(), it.rxErrors.toLong(), it.txErrors.toLong(),
                it.rxDropped.toLong(), it.txDropped.toLong(), it.observedAt.instant(clock),
            )
        },
        userTraffic = userTrafficList.map {
            UserTrafficInput(it.userId, it.rxBytes.toLong(), it.txBytes.toLong(), it.observedAt.instant(clock))
        },
        events = eventsList.map { RuntimeEventInput(it.type, it.occurredAt.instant(clock), it.payloadJson) },
    )

    /** Нода опознаётся по CN сертификата: перехватчик кладёт его в контекст вызова. */
    private fun authenticatedNodeId(): String = NodeIdentityInterceptor.currentNodeId()
        ?: throw StatusRuntimeException(Status.UNAUTHENTICATED.withDescription("client certificate is required"))

    private companion object {
        /** Повтор текущей ревизии на случай потерянного уведомления. */
        val HEARTBEAT: Duration = Duration.ofSeconds(30)
        val log = LoggerFactory.getLogger(NodeControlGrpcService::class.java)
    }
}

private fun Instant.timestamp(): Timestamp =
    Timestamp.newBuilder().setSeconds(epochSecond).setNanos(nano).build()

/** Пустая метка означает «сейчас»: часы ноды авторитетом не являются. */
private fun Timestamp.instant(clock: Clock): Instant =
    if (seconds == 0L && nanos == 0) clock.instant() else Instant.ofEpochSecond(seconds, nanos.toLong())

private fun Timestamp.instantOrNull(): Instant? =
    if (seconds == 0L && nanos == 0) null else Instant.ofEpochSecond(seconds, nanos.toLong())
