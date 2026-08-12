package io.boardproxy.control.provisioning.infrastructure.config

import io.boardproxy.control.provisioning.application.CatalogRepository
import io.boardproxy.control.provisioning.application.CatalogSnapshotRepository
import org.springframework.boot.ApplicationArguments
import org.springframework.boot.ApplicationRunner
import org.springframework.stereotype.Component

@Component
class CatalogSnapshotBackfill(
    private val catalogs: CatalogRepository,
    private val snapshots: CatalogSnapshotRepository,
) : ApplicationRunner {
    override fun run(args: ApplicationArguments) {
        var offset = 0
        while (true) {
            val page = catalogs.search(null, offset, BATCH_SIZE)
            page.forEach(snapshots::save)
            if (page.size < BATCH_SIZE) return
            offset += page.size
        }
    }

    private companion object {
        const val BATCH_SIZE = 200
    }
}
