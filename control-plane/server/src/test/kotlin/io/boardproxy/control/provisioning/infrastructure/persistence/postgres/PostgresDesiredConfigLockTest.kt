package io.boardproxy.control.provisioning.infrastructure.persistence.postgres

import io.boardproxy.control.testing.PostgresSupport
import io.boardproxy.control.testing.testNode
import org.springframework.jdbc.datasource.DataSourceTransactionManager
import org.springframework.transaction.support.TransactionTemplate
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class PostgresDesiredConfigLockTest {
    private val nodes = PostgresNodeRepository(PostgresSupport.named, PostgresSupport.json, PostgresSupport.cipher)
    private val configs = PostgresDesiredConfigRepository(PostgresSupport.named, PostgresSupport.cipher)

    @BeforeTest
    fun prepare() {
        assertTrue(PostgresSupport.dockerAvailable, "тест блокировки требует Docker")
        PostgresSupport.truncate()
        nodes.create(testNode("node-1"))
    }

    @Test
    fun `две публикации одной ноды сериализуются`() {
        val transaction = TransactionTemplate(DataSourceTransactionManager(PostgresSupport.dataSource))
        val firstLocked = CountDownLatch(1)
        val releaseFirst = CountDownLatch(1)
        val secondAttempted = CountDownLatch(1)
        val secondLocked = CountDownLatch(1)

        Executors.newFixedThreadPool(2).use { executor ->
            val first = executor.submit {
                transaction.executeWithoutResult {
                    configs.lock("node-1")
                    firstLocked.countDown()
                    assertTrue(releaseFirst.await(5, TimeUnit.SECONDS))
                }
            }
            assertTrue(firstLocked.await(5, TimeUnit.SECONDS))

            val second = executor.submit {
                transaction.executeWithoutResult {
                    secondAttempted.countDown()
                    configs.lock("node-1")
                    secondLocked.countDown()
                }
            }
            assertTrue(secondAttempted.await(5, TimeUnit.SECONDS))
            assertFalse(secondLocked.await(200, TimeUnit.MILLISECONDS), "вторая транзакция не должна пройти lock")

            releaseFirst.countDown()
            assertTrue(secondLocked.await(5, TimeUnit.SECONDS))
            first.get(5, TimeUnit.SECONDS)
            second.get(5, TimeUnit.SECONDS)
        }
    }
}
