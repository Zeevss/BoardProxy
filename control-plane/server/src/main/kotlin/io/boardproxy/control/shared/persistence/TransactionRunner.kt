package io.boardproxy.control.shared.persistence

interface TransactionRunner {
    fun <T> required(block: () -> T): T
}
