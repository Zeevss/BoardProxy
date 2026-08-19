package io.boardproxy.control.provisioning.domain.model

import io.boardproxy.control.shared.crypto.X25519
import io.boardproxy.control.shared.crypto.base64Url

/**
 * Собирает `bproxy://` ссылку пользователя на конкретной ноде.
 *
 * Ссылка нигде не хранится: она всегда выводится из ключей, поэтому отзыв
 * пользователя или борда действует немедленно. null означает, что рабочей
 * ссылки сейчас не существует — нода или пользователь выключены, квота
 * исчерпана, доступных бордов нет, либо приватным ключом владеем не мы.
 *
 * [boards] — борды ноды, на которые у пользователя есть грант; их состояние
 * проверяется здесь, чтобы вызывающему не приходилось повторять фильтр.
 */
fun keylinkFor(
    node: Node,
    placement: UserOnNode,
    boards: List<Board>,
    label: String,
): String? {
    if (!node.state.isEnabled || !placement.enabled) return null
    val privateKey = placement.user.privateKey ?: return null

    val hashes = boards
        .filter { it.nodeId == node.id && it.id in placement.boardIds && it.state.isEnabled }
        .map(Board::hash)
        .sorted()
    if (hashes.isEmpty()) return null

    val clientPrivate = KeyMaterial.decodePrivate(privateKey)
    val serverPublic = X25519.publicKeyOf(KeyMaterial.decodePrivate(node.core.server.privateKey))
    val payload = (clientPrivate + serverPublic).base64Url()
    return "bproxy://$payload@${hashes.joinToString(",")}#$label"
}
