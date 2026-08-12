package io.boardproxy.control.shared.events

import org.springframework.http.ResponseEntity
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PathVariable
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RequestParam
import org.springframework.web.bind.annotation.RestController

@RestController
@RequestMapping("/api/v1/operations/outbox")
@PreAuthorize("hasRole('ADMIN')")
class OutboxController(private val operations: OutboxOperations) {
    @GetMapping("/dead-letters")
    fun deadLetters(@RequestParam(defaultValue = "100") limit: Int) = operations.deadLetters(limit)

    @PostMapping("/dead-letters/{eventId}/retry")
    fun retry(@PathVariable eventId: String): ResponseEntity<Void> {
        operations.retry(eventId)
        return ResponseEntity.accepted().build()
    }
}
