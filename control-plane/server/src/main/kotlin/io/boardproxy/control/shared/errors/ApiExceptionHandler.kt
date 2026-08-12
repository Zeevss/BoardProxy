package io.boardproxy.control.shared.errors

import org.springframework.http.HttpStatus
import org.springframework.http.ProblemDetail
import org.springframework.web.bind.annotation.ExceptionHandler
import org.springframework.web.bind.annotation.RestControllerAdvice

@RestControllerAdvice
class ApiExceptionHandler {
    @ExceptionHandler(ResourceNotFound::class)
    fun notFound(error: ResourceNotFound): ProblemDetail = problem(HttpStatus.NOT_FOUND, error)

    @ExceptionHandler(ResourceConflict::class)
    fun conflict(error: ResourceConflict): ProblemDetail = problem(HttpStatus.CONFLICT, error)

    @ExceptionHandler(InvalidRequest::class, IllegalArgumentException::class)
    fun badRequest(error: RuntimeException): ProblemDetail = problem(HttpStatus.BAD_REQUEST, error)

    private fun problem(status: HttpStatus, error: RuntimeException) =
        ProblemDetail.forStatusAndDetail(status, error.message ?: status.reasonPhrase)
}
