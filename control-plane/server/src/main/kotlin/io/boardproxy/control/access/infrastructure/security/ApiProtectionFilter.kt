package io.boardproxy.control.access.infrastructure.security

import jakarta.servlet.FilterChain
import jakarta.servlet.ReadListener
import jakarta.servlet.ServletInputStream
import jakarta.servlet.http.HttpServletRequest
import jakarta.servlet.http.HttpServletRequestWrapper
import jakarta.servlet.http.HttpServletResponse
import org.springframework.http.MediaType
import org.springframework.web.filter.OncePerRequestFilter
import java.io.BufferedReader
import java.io.InputStreamReader
import java.io.IOException
import java.time.Clock
import java.util.concurrent.ConcurrentHashMap

class ApiProtectionFilter(
    private val requestsPerMinute: Int,
    private val maximumBodyBytes: Long,
    private val clock: Clock,
    private val maximumKeys: Int = MAX_KEYS,
) : OncePerRequestFilter() {
    private val windows = ConcurrentHashMap<String, Window>()

    override fun shouldNotFilter(request: HttpServletRequest): Boolean =
        !request.requestURI.startsWith("/api/") && !request.requestURI.startsWith("/actuator/")

    override fun doFilterInternal(
        request: HttpServletRequest,
        response: HttpServletResponse,
        filterChain: FilterChain,
    ) {
        if (request.contentLengthLong > maximumBodyBytes) {
            problem(response, 413, "Payload Too Large")
            return
        }
        val minute = clock.instant().epochSecond / 60
        // Фильтр стоит до authentication: мусорный bearer не должен создавать
        // отдельный bucket или обходить ограничение. remoteAddr берётся у
        // контейнера, а не из подделываемого X-Forwarded-For.
        val key = "ip:${request.remoteAddr}"
        if (!reserveKey(key, minute)) {
            response.setHeader("Retry-After", "60")
            problem(response, 429, "Too Many Requests")
            return
        }
        val window = windows.compute(key) { _, current ->
            if (current == null || current.minute != minute) Window(minute, 1) else current.copy(count = current.count + 1)
        }
        if (requireNotNull(window).count > requestsPerMinute) {
            response.setHeader("Retry-After", "60")
            problem(response, 429, "Too Many Requests")
            return
        }
        try {
            filterChain.doFilter(LimitedRequest(request, maximumBodyBytes), response)
        } catch (_: PayloadTooLarge) {
            if (!response.isCommitted) {
                response.reset()
                problem(response, 413, "Payload Too Large")
            }
        }
    }

    private fun problem(response: HttpServletResponse, status: Int, title: String) {
        response.status = status
        response.contentType = MediaType.APPLICATION_PROBLEM_JSON_VALUE
        response.writer.write("""{"status":$status,"title":"$title"}""")
    }

    private fun reserveKey(key: String, minute: Long): Boolean {
        if (windows.containsKey(key)) return true
        synchronized(windows) {
            if (windows.containsKey(key)) return true
            windows.entries.removeIf { it.value.minute < minute }
            if (windows.size >= maximumKeys) return false
            windows[key] = Window(minute, 0)
            return true
        }
    }

    private data class Window(val minute: Long, val count: Int)

    private class PayloadTooLarge : IOException("request body exceeds configured limit")

    private class LimitedRequest(request: HttpServletRequest, private val limit: Long) : HttpServletRequestWrapper(request) {
        override fun getInputStream(): ServletInputStream {
            val delegate = super.getInputStream()
            return object : ServletInputStream() {
                private var read = 0L
                override fun read(): Int = delegate.read().also { if (it >= 0) account(1) }
                override fun read(buffer: ByteArray, offset: Int, length: Int): Int =
                    delegate.read(buffer, offset, length).also { if (it > 0) account(it.toLong()) }
                override fun isFinished(): Boolean = delegate.isFinished
                override fun isReady(): Boolean = delegate.isReady
                override fun setReadListener(listener: ReadListener?) = delegate.setReadListener(listener)
                private fun account(bytes: Long) {
                    read += bytes
                    if (read > limit) throw PayloadTooLarge()
                }
            }
        }

        override fun getReader(): BufferedReader = BufferedReader(
            InputStreamReader(inputStream, characterEncoding?.let(java.nio.charset.Charset::forName) ?: Charsets.UTF_8),
        )
    }

    private companion object {
        const val MAX_KEYS = 100_000
    }
}
