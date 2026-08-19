package io.boardproxy.control.delivery.infrastructure.config

import io.boardproxy.control.delivery.api.grpc.NodeControlGrpcService
import io.boardproxy.control.delivery.api.grpc.NodeIdentityInterceptor
import io.boardproxy.control.fleet.application.ServerCertificateAuthority
import io.boardproxy.control.fleet.application.NodeConnectionPolicy
import io.grpc.Server
import io.grpc.ServerInterceptors
import io.grpc.netty.shaded.io.grpc.netty.GrpcSslContexts
import io.grpc.netty.shaded.io.grpc.netty.NettyServerBuilder
import io.grpc.netty.shaded.io.netty.handler.ssl.ClientAuth
import io.grpc.netty.shaded.io.netty.handler.ssl.SslContextBuilder
import org.slf4j.LoggerFactory
import org.springframework.beans.factory.annotation.Value
import org.springframework.context.SmartLifecycle
import org.springframework.stereotype.Component
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

@Component
class NodeGrpcServer(
    private val service: NodeControlGrpcService,
    private val authority: ServerCertificateAuthority,
    private val connectionPolicy: NodeConnectionPolicy,
    @Value("\${control.grpc.port}") private val port: Int,
) : SmartLifecycle {
    private val log = LoggerFactory.getLogger(javaClass)
    private var server: Server? = null

    override fun start() {
        if (isRunning) return
        val ssl = GrpcSslContexts.configure(
            SslContextBuilder.forServer(authority.serverPrivateKey, authority.serverCertificate),
        )
            .trustManager(authority.caCertificate)
            .clientAuth(ClientAuth.OPTIONAL)
            .protocols("TLSv1.3")
            .build()
        server = NettyServerBuilder.forPort(port)
            .sslContext(ssl)
            // Обработчики блокирующие: Watch ждёт сигнала, остальные — JDBC.
            // На виртуальных потоках это стоит столько же, сколько корутины,
            // и не требует ни одной из них.
            .executor(Executors.newVirtualThreadPerTaskExecutor())
            .addService(ServerInterceptors.intercept(service, NodeIdentityInterceptor(connectionPolicy)))
            .build()
            .start()
        log.info("node gRPC listening on port {} with TLS 1.3", port)
    }

    override fun stop() {
        server?.shutdown()
        if (server?.awaitTermination(10, TimeUnit.SECONDS) == false) server?.shutdownNow()
        server = null
    }

    override fun isRunning(): Boolean = server?.isShutdown == false
    override fun getPhase(): Int = Int.MAX_VALUE - 100
}
