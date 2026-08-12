package io.boardproxy.control.telemetry.application

import java.time.Instant

interface TrafficMaintenance {
    fun rebuildHourly(from: Instant, to: Instant): Int
    fun deleteRawBefore(cutoff: Instant): Int
    fun deleteRollupsBefore(cutoff: Instant): Int
}
