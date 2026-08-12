package io.boardproxy.control.provisioning.application

import io.boardproxy.control.provisioning.domain.model.Catalog

fun interface CoreConfigCompiler {
    fun compile(catalog: Catalog): ByteArray
}
