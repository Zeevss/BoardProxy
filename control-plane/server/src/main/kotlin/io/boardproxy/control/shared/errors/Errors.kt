package io.boardproxy.control.shared.errors

class ResourceNotFound(message: String) : RuntimeException(message)
class ResourceConflict(message: String) : RuntimeException(message)
class InvalidRequest(message: String) : RuntimeException(message)
class ResourceForbidden(message: String) : RuntimeException(message)
class ResourceGone(message: String) : RuntimeException(message)
