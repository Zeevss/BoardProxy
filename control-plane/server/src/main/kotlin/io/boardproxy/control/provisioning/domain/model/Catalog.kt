package io.boardproxy.control.provisioning.domain.model

import java.time.Instant

data class AssignedUserResources(val user: User, val boardIds: List<String>)

data class AssignedResources(
    val boards: List<Board>,
    val users: List<AssignedUserResources>,
)

data class Catalog(
    val node: Node,
    val boards: List<Board>,
    val users: List<User>,
    val assignment: NodeAssignment,
    val version: Long,
    val updatedAt: Instant,
) {
    init {
        validateAggregate()
    }

    fun assignedResources(): AssignedResources {
        val boardById = boards.associateBy(Board::id)
        val userById = users.associateBy(User::id)
        return AssignedResources(
            boards = assignment.boardIds.map(boardById::getValue).sortedBy(Board::id),
            users = assignment.users.map {
                AssignedUserResources(userById.getValue(it.userId), it.boardIds.sorted())
            }.sortedBy { it.user.id },
        )
    }

    private fun validateAggregate() {
        requireDomain(version > 0, "catalog version must be positive")
        requireDomain(assignment.nodeId == node.id, "assignment must reference the node")
        requireDomain(boards.map(Board::id).distinct().size == boards.size, "duplicate board id")
        requireDomain(boards.map(Board::hash).distinct().size == boards.size, "duplicate board hash")
        requireDomain(users.map(User::id).distinct().size == users.size, "duplicate user id")
        requireDomain(users.map(User::identityFingerprint).distinct().size == users.size, "duplicate user identity")

        val boardIds = boards.map(Board::id).toSet()
        val userIds = users.map(User::id).toSet()
        requireDomain(assignment.boardIds.isNotEmpty(), "assignment must contain a board")
        requireDomain(assignment.boardIds.distinct().size == assignment.boardIds.size, "duplicate assigned board")
        requireDomain(assignment.boardIds.all(boardIds::contains), "assignment references unknown board")
        requireDomain(assignment.users.map(AssignedUser::userId).distinct().size == assignment.users.size, "duplicate assigned user")
        assignment.users.forEach { assigned ->
            requireDomain(assigned.userId in userIds, "assignment references unknown user")
            requireDomain(assigned.boardIds.isNotEmpty(), "assigned user must contain a board")
            requireDomain(assigned.boardIds.distinct().size == assigned.boardIds.size, "duplicate user board")
            requireDomain(
                assigned.boardIds.all(assignment.boardIds::contains),
                "user references board outside node assignment",
            )
        }
    }
}
