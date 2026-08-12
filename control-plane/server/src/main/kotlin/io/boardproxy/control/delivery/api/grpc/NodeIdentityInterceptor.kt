package io.boardproxy.control.delivery.api.grpc

import io.grpc.Context
import io.grpc.Contexts
import io.grpc.Grpc
import io.grpc.Metadata
import io.grpc.ServerCall
import io.grpc.ServerCallHandler
import io.grpc.ServerInterceptor
import java.security.cert.X509Certificate
import io.boardproxy.control.fleet.application.NodeConnectionPolicy
import javax.naming.ldap.LdapName

class NodeIdentityInterceptor(private val policy: NodeConnectionPolicy) : ServerInterceptor {
    override fun <ReqT : Any, RespT : Any> interceptCall(
        call: ServerCall<ReqT, RespT>,
        headers: Metadata,
        next: ServerCallHandler<ReqT, RespT>,
    ): ServerCall.Listener<ReqT> {
        val identity = runCatching {
            val session = requireNotNull(call.attributes.get(Grpc.TRANSPORT_ATTR_SSL_SESSION))
            val certificate = session.peerCertificates.first() as X509Certificate
            val nodeId = LdapName(certificate.subjectX500Principal.name).rdns
                .firstOrNull { it.type.equals("CN", ignoreCase = true) }?.value?.toString()
            nodeId?.takeIf { policy.authorize(it, certificate) }
        }.getOrNull()
        val context = identity?.let { Context.current().withValue(NODE_ID, it) } ?: Context.current()
        return Contexts.interceptCall(context, call, headers, next)
    }

    companion object {
        private val NODE_ID: Context.Key<String> = Context.key("boardproxy-node-id")
        fun currentNodeId(): String? = NODE_ID.get()
    }
}
