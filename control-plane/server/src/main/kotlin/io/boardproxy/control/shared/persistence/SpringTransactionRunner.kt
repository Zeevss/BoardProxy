package io.boardproxy.control.shared.persistence

import org.springframework.stereotype.Component
import org.springframework.transaction.support.TransactionTemplate

@Component
class SpringTransactionRunner(private val transactions: TransactionTemplate) : TransactionRunner {
    override fun <T> required(block: () -> T): T = requireNotNull(transactions.execute { block() })
}
