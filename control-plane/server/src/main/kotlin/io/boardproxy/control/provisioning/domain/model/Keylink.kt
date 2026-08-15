package io.boardproxy.control.provisioning.domain.model

import java.math.BigInteger
import java.security.KeyFactory
import java.security.spec.NamedParameterSpec
import java.security.spec.XECPrivateKeySpec
import java.security.spec.XECPublicKeySpec
import java.util.Base64
import javax.crypto.KeyAgreement

fun Catalog.keylinkFor(userId: String, label: String): String? {
    val user = users.firstOrNull { it.id == userId } ?: return null
    val assigned = assignment.users.firstOrNull { it.userId == userId } ?: return null
    val hashes = assigned.boardIds.mapNotNull { boardId ->
        boards.firstOrNull { it.id == boardId && it.state.isEnabled }?.hash
    }.sorted()
    if (!node.state.isEnabled || !user.state.isEnabled || hashes.isEmpty() || user.privateKey == null) return null
    val clientPrivate = KeyMaterial.decodePrivate(user.privateKey)
    val serverPrivate = KeyMaterial.decodePrivate(node.core.server.privateKey)
    val serverPublic = x25519Public(serverPrivate)
    val payload = Base64.getUrlEncoder().withoutPadding().encodeToString(clientPrivate + serverPublic)
    return "bproxy://$payload@${hashes.joinToString(",")}#$label"
}

private fun x25519Public(privateKey: ByteArray): ByteArray {
    val parameters = NamedParameterSpec.X25519
    val decodedPrivate = KeyFactory.getInstance("X25519")
        .generatePrivate(XECPrivateKeySpec(parameters, privateKey))
    val basePoint = KeyFactory.getInstance("X25519")
        .generatePublic(XECPublicKeySpec(parameters, BigInteger.valueOf(9)))
    return KeyAgreement.getInstance("X25519").run {
        init(decodedPrivate)
        doPhase(basePoint, true)
        generateSecret()
    }
}
