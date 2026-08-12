package io.boardproxy.control.activity.api.rest

import org.springframework.http.MediaType
import org.springframework.security.access.prepost.PreAuthorize
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController
import org.springframework.web.servlet.mvc.method.annotation.SseEmitter

@RestController
@RequestMapping("/api/v1/events")
class FrontendEventsController(private val events: FrontendEventStream) {
    @GetMapping(produces = [MediaType.TEXT_EVENT_STREAM_VALUE])
    @PreAuthorize("hasAnyRole('VIEWER', 'OPERATOR', 'ADMIN')")
    fun stream(): SseEmitter = events.open()
}
