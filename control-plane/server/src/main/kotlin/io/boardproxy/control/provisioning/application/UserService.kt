package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.Grant
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.provisioning.domain.model.User
import io.boardproxy.control.provisioning.domain.model.generatePrivateKey
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.errors.ResourceNotFound
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.shared.audit.AuditEvent
import io.boardproxy.control.shared.audit.AuditRepository
import java.time.Clock
import java.util.UUID

data class UserInput(
    val id: String? = null,
    val name: String,
    val description: String = "",
    /** Если публичный ключ не задан, хаб выпускает приватный сам и умеет собрать keylink. */
    val publicKey: String? = null,
    val state: ResourceState = ResourceState.ENABLED,
    /** Лимит применяется каждым core независимо; это не fleet-wide semaphore. */
    val maxSessions: Int = 0,
    val maxLanes: Int = 4,
)

/** Размещение на одной ноде; пустой набор бордов означает «все включённые борды ноды». */
data class GrantInput(val nodeId: String, val boardIds: Set<String> = emptySet())

class UserService(
    private val users: UserRepository,
    private val boards: BoardRepository,
    private val grants: GrantRepository,
    private val publisher: DesiredConfigPublisher,
    private val transactions: TransactionRunner,
    private val clock: Clock,
    private val audit: AuditRepository = AuditRepository { },
    private val nextId: () -> String = { UUID.randomUUID().toString() },
) {
    fun get(id: String): User = users.find(id) ?: throw ResourceNotFound("user $id not found")

    fun list(query: String?, nodeId: String?, offset: Int, limit: Int): Page<User> =
        Page(users.list(query, nodeId, offset, limit), offset, limit, users.count(query, nodeId))

    fun grantsOf(id: String): List<Grant> {
        get(id)
        return grants.of(id)
    }

    /** Размещения сразу для страницы списка — без запроса на каждую строку. */
    fun nodesOfAll(userIds: Collection<String>): Map<String, Set<String>> = grants.nodesOfAll(userIds)

    fun create(input: UserInput, actor: String): User = transactions.required {
        val id = input.id?.trim().orEmpty().ifBlank { throw InvalidRequest("user id is required") }
        if (users.find(id) != null) throw ResourceConflict("user $id already exists")
        val user = User(
            id = id,
            name = input.name.trim(),
            description = input.description.trim(),
            privateKey = if (input.publicKey == null) generatePrivateKey() else null,
            publicKey = input.publicKey,
            state = input.state,
            maxSessions = input.maxSessions,
            maxLanes = input.maxLanes,
            version = 1,
            updatedAt = clock.instant(),
        )
        if (users.findByFingerprint(user.identityFingerprint()) != null) {
            throw ResourceConflict("user identity already exists")
        }
        users.create(user)
        audit(user, "user.created", actor)
        user
    }

    /**
     * Ключ не переиздаётся правкой: он общий для всех нод и для всех выданных
     * подписок. Ротация — отдельная операция.
     */
    fun update(id: String, expectedVersion: Long, input: UserInput, actor: String): User = transactions.required {
        val current = get(id)
        val updated = current.copy(
            name = input.name.trim(),
            description = input.description.trim(),
            state = input.state,
            maxSessions = input.maxSessions,
            maxLanes = input.maxLanes,
            version = expectedVersion + 1,
            updatedAt = clock.instant(),
        )
        if (!users.replace(updated, expectedVersion)) throw ResourceConflict("user $id version changed")
        audit(updated, "user.updated", actor)
        publisher.publish(grants.nodesOf(id), "user.updated", actor)
        updated
    }

    /** Обесценивает все выданные пользователю ссылки на всех нодах сразу. */
    fun rotateKey(id: String, expectedVersion: Long, actor: String): User = transactions.required {
        val current = get(id)
        if (current.privateKey == null) throw InvalidRequest("user $id has no hub-issued key to rotate")
        val rotated = current.copy(
            privateKey = generatePrivateKey(),
            version = expectedVersion + 1,
            updatedAt = clock.instant(),
        )
        if (!users.replace(rotated, expectedVersion)) throw ResourceConflict("user $id version changed")
        audit(rotated, "user.key-rotated", actor)
        publisher.publish(grants.nodesOf(id), "user.key-rotated", actor)
        rotated
    }

    fun delete(id: String, expectedVersion: Long, actor: String) = transactions.required {
        val current = get(id)
        if (current.version != expectedVersion) throw ResourceConflict("user $id version changed")
        // Ноды запоминаются до удаления: после каскада гранты уже не прочитать.
        val affected = grants.nodesOf(id)
        audit(current, "user.deleted", actor)
        if (!users.delete(id)) throw ResourceNotFound("user $id not found")
        publisher.publish(affected, "user.deleted", actor)
    }

    /**
     * Размещения заменяются целиком. Пересобираются конфигурации и тех нод, где
     * пользователь появился, и тех, откуда он ушёл, — иначе на покинутой ноде
     * он остался бы жить до следующей случайной правки.
     */
    fun replaceGrants(id: String, placements: List<GrantInput>, actor: String): List<Grant> = transactions.required {
        val user = get(id)
        val previous = grants.nodesOf(id)
        val resolved = placements.map { placement ->
            val available = boards.listByNode(placement.nodeId)
            if (available.isEmpty()) throw InvalidRequest("node ${placement.nodeId} has no boards")
            val boardIds = placement.boardIds.ifEmpty {
                available.filter { it.state == ResourceState.ENABLED }.map { it.id }.toSet()
            }
            val unknown = boardIds - available.map { it.id }.toSet()
            if (unknown.isNotEmpty()) {
                throw InvalidRequest("boards $unknown do not belong to node ${placement.nodeId}")
            }
            Grant(id, placement.nodeId, boardIds)
        }
        grants.replace(id, resolved)
        audit(user, "user.grants-changed", actor, resourceType = "user-grants")
        publisher.publish(previous + resolved.map(Grant::nodeId), "user.grants-changed", actor)
        resolved
    }

    private fun audit(user: User, action: String, actor: String, resourceType: String = "user") = audit.append(
        AuditEvent(
            id = nextId(), nodeId = null, actor = actor, action = action,
            resourceType = resourceType, resourceId = user.id, resourceVersion = user.version,
            occurredAt = clock.instant(),
        ),
    )
}
