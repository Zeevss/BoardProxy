package io.boardproxy.control.delivery.domain

import java.time.Instant

data class AppliedState(val revision: Long = 0, val sha256: String = "")

data class NodeHello(
    val bootId: String,
    val agentVersion: String,
    val coreVersion: String,
    val appliedRevision: Long,
    val configSha256: String,
)

data class ApplyOutcome(
    val desiredRevision: Long,
    val runtimeRevision: Long,
    val configSha256: String,
    val error: String,
    val appliedAt: Instant,
)

data class Heartbeat(
    val sampledAt: Instant,
    val coreRunning: Boolean,
    val coreReady: Boolean,
    val appliedRevision: Long,
    val error: String,
)

data class NodeStatus(
    val nodeId: String,
    val connected: Boolean = false,
    val bootId: String? = null,
    val agentVersion: String? = null,
    val coreVersion: String? = null,
    val coreRunning: Boolean = false,
    val coreReady: Boolean = false,
    val desiredRevision: Long = 0,
    val appliedRevision: Long = 0,
    val configSha256: String? = null,
    val lastError: String? = null,
    val lastSeen: Instant? = null,
    val lastApply: ApplyOutcome? = null,
    val fencingToken: Long = 0,
    val version: Long = 0,
)
