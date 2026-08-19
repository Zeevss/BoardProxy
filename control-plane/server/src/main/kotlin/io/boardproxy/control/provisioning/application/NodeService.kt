package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.CoreSettings
import io.boardproxy.control.provisioning.domain.model.Node
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.provisioning.domain.model.generatePrivateKey
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.errors.ResourceNotFound
import io.boardproxy.control.shared.persistence.TransactionRunner
import java.time.Clock

/**
 * Настройки ядра приходят целиком; приватный ключ сервера в них не входит —
 * его выпускает хаб при создании ноды и наружу больше не отдаёт.
 */
data class NodeInput(
    val id: String? = null,
    val name: String,
    val state: ResourceState = ResourceState.ENABLED,
    val settings: CoreSettings? = null,
)

class NodeService(
    private val nodes: NodeRepository,
    private val publisher: DesiredConfigPublisher,
    private val transactions: TransactionRunner,
    private val clock: Clock,
) {
    fun get(id: String): Node = nodes.find(id) ?: throw ResourceNotFound("node $id not found")

    fun list(query: String?, offset: Int, limit: Int): Page<Node> =
        Page(nodes.list(query, offset, limit), offset, limit, nodes.count(query))

    fun create(input: NodeInput, actor: String): Node {
        val id = input.id?.trim().orEmpty().ifBlank { throw InvalidRequest("node id is required") }
        val now = clock.instant()
        // Ключ выпускается здесь и только здесь: то, что пришло в настройках с
        // запросом, доверенным источником случайности не является.
        val serverKey = generatePrivateKey()
        val settings = input.settings ?: CoreSettings.defaults(serverKey)
        val node = Node(
            id = id,
            name = input.name.trim(),
            state = input.state,
            core = settings.copy(server = settings.server.copy(privateKey = serverKey)),
            version = 1,
            updatedAt = now,
        )
        return transactions.required {
            if (nodes.find(id) != null) throw ResourceConflict("node $id already exists")
            nodes.create(node)
            publisher.publish(setOf(id), "node.created", actor)
            node
        }
    }

    /**
     * Ключ сервера переносится из текущей записи: смена ключа обесценила бы все
     * выданные keylink'и, поэтому случайно поменять его правкой настроек нельзя.
     */
    fun update(id: String, expectedVersion: Long, input: NodeInput, actor: String): Node = transactions.required {
        val current = get(id)
        val settings = input.settings ?: current.core
        val updated = current.copy(
            name = input.name.trim(),
            state = input.state,
            core = settings.copy(server = settings.server.copy(privateKey = current.core.server.privateKey)),
            version = expectedVersion + 1,
            updatedAt = clock.instant(),
        )
        if (!nodes.replace(updated, expectedVersion)) throw ResourceConflict("node $id version changed")
        publisher.publish(setOf(id), "node.updated", actor)
        updated
    }

    /**
     * Полное удаление, а не отзыв: борды, гранты, конфигурация, снимки, статус и
     * телеметрия уходят каскадом. Прежняя схема удалять ноду не умела вовсе —
     * состояние revoked было единственным способом её погасить.
     */
    fun delete(id: String, expectedVersion: Long, actor: String) = transactions.required {
        val current = get(id)
        if (current.version != expectedVersion) throw ResourceConflict("node $id version changed")
        if (!nodes.delete(id)) throw ResourceNotFound("node $id not found")
    }
}
