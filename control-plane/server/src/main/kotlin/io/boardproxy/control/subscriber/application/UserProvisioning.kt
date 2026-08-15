package io.boardproxy.control.subscriber.application

import io.boardproxy.control.provisioning.application.CatalogCommands
import io.boardproxy.control.provisioning.application.CatalogQueries
import io.boardproxy.control.provisioning.domain.model.AssignedUser
import io.boardproxy.control.provisioning.domain.model.NodeAssignment
import io.boardproxy.control.provisioning.domain.model.ResourceState
import io.boardproxy.control.provisioning.domain.model.User
import io.boardproxy.control.provisioning.domain.model.keylinkFor
import io.boardproxy.control.shared.errors.InvalidRequest
import io.boardproxy.control.shared.errors.ResourceConflict
import io.boardproxy.control.shared.persistence.TransactionRunner
import io.boardproxy.control.subscription.application.SubscriptionCommands
import io.boardproxy.control.subscription.application.SubscriptionDraft
import io.boardproxy.control.subscription.application.SubscriptionKeyDraft
import io.boardproxy.control.subscription.application.SubscriptionLinkBuilder
import java.security.SecureRandom
import java.time.Clock
import java.util.Base64

data class UserProvisioningRequest(
    val id: String,
    val name: String,
    val targets: List<UserTarget>,
    val maxSessions: Int,
    val maxLanes: Int,
)

data class UserTarget(val nodeId: String, val boardIds: List<String>, val keyName: String?)

data class ProvisionedUser(
    val id: String,
    val name: String,
    val delivery: UserAccessDelivery,
)

data class UserAccessDelivery(
    val type: String,
    val subscriptionId: String? = null,
    val subscriptionUrl: String? = null,
    val keys: List<ProvisionedKey> = emptyList(),
)

data class ProvisionedKey(val id: String, val name: String, val nodeId: String, val keylink: String)

fun interface UserProvisioningCommands {
    fun create(request: UserProvisioningRequest, actor: String): ProvisionedUser
}

class UserProvisioningService(
    private val catalogs: CatalogQueries,
    private val catalogCommands: CatalogCommands,
    private val subscriptions: SubscriptionCommands,
    private val links: SubscriptionLinkBuilder,
    private val transactions: TransactionRunner,
    private val clock: Clock,
    private val random: SecureRandom = SecureRandom(),
) : UserProvisioningCommands {
    override fun create(request: UserProvisioningRequest, actor: String): ProvisionedUser {
        val userId = request.id.trim()
        val name = request.name.trim()
        if (actor.isBlank()) throw InvalidRequest("actor is required")
        if (userId.isBlank() || name.isBlank()) throw InvalidRequest("user id and name are required")
        if (request.targets.isEmpty()) throw InvalidRequest("user must target at least one node")
        if (request.targets.map { it.nodeId }.distinct().size != request.targets.size) {
            throw InvalidRequest("duplicate target node")
        }
        if (request.maxSessions < 0 || request.maxLanes !in 1..32) throw InvalidRequest("invalid user limits")

        val now = clock.instant()
        val privateKey = "base64:" + Base64.getEncoder().encodeToString(ByteArray(32).also(random::nextBytes))
        val candidates = request.targets.map { target ->
            val current = catalogs.get(target.nodeId)
            if (current.users.any { it.id == userId }) throw ResourceConflict("user $userId already exists on ${target.nodeId}")
            if (target.boardIds.isEmpty()) throw InvalidRequest("target ${target.nodeId} must contain a board")
            if (target.boardIds.distinct().size != target.boardIds.size) throw InvalidRequest("duplicate board on ${target.nodeId}")
            val assignedBoards = current.assignment.boardIds.toSet()
            if (!target.boardIds.all(assignedBoards::contains)) {
                throw InvalidRequest("target ${target.nodeId} references a board not assigned to the node")
            }
            val user = User(
                id = userId, name = name, privateKey = privateKey, state = ResourceState.ENABLED,
                maxSessions = request.maxSessions, maxLanes = request.maxLanes, version = 1, updatedAt = now,
            )
            target to current.copy(
                users = current.users + user,
                assignment = NodeAssignment(
                    nodeId = current.node.id,
                    boardIds = current.assignment.boardIds,
                    users = current.assignment.users + AssignedUser(userId, target.boardIds),
                    version = current.assignment.version + 1,
                    updatedAt = now,
                ),
                version = current.version + 1,
                updatedAt = now,
            )
        }

        return transactions.required {
            candidates.forEach { (_, candidate) ->
                catalogCommands.replace(candidate, candidate.version - 1, actor, "user.provisioned")
            }
            val keyDrafts = candidates.map { (target, catalog) ->
                SubscriptionKeyDraft(
                    id = target.nodeId,
                    name = target.keyName?.trim().takeUnless { it.isNullOrEmpty() } ?: catalog.node.name,
                    nodeId = target.nodeId,
                    userId = userId,
                )
            }
            val delivery = if (links.enabled) {
                val issued = subscriptions.create(SubscriptionDraft(name, keyDrafts), actor)
                UserAccessDelivery(
                    type = "subscription", subscriptionId = issued.subscription.id,
                    subscriptionUrl = links.build(issued),
                )
            } else {
                UserAccessDelivery(
                    type = "keylinks",
                    keys = candidates.mapIndexed { index, (_, catalog) ->
                        val draft = keyDrafts[index]
                        ProvisionedKey(
                            id = draft.id, name = draft.name, nodeId = draft.nodeId,
                            keylink = requireNotNull(catalog.keylinkFor(userId, draft.name)),
                        )
                    },
                )
            }
            ProvisionedUser(userId, name, delivery)
        }
    }
}
