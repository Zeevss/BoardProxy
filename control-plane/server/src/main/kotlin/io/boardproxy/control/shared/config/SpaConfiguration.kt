package io.boardproxy.control.shared.config

import org.springframework.context.annotation.Configuration
import org.springframework.core.io.ClassPathResource
import org.springframework.core.io.Resource
import org.springframework.web.servlet.config.annotation.ResourceHandlerRegistry
import org.springframework.web.servlet.config.annotation.WebMvcConfigurer
import org.springframework.web.servlet.resource.PathResourceResolver

/**
 * Отдача панели как одностраничного приложения.
 *
 * Панель роутится на клиенте, поэтому `/nodes` и `/users/u-alice` — это не
 * файлы, а состояния одного и того же `index.html`. Без этой развязки
 * перезагрузка страницы на любом экране, кроме корня, отдавала бы 404.
 *
 * Запросы к API сюда не попадают: [isApplicationPath] пропускает их дальше по
 * цепочке, чтобы несуществующий эндпоинт остался честным 404, а не превратился
 * в HTML.
 */
@Configuration
class SpaConfiguration : WebMvcConfigurer {

    override fun addResourceHandlers(registry: ResourceHandlerRegistry) {
        registry.addResourceHandler("/**")
            .addResourceLocations(STATIC_ROOT)
            .resourceChain(true)
            .addResolver(SpaResourceResolver())
    }

    private class SpaResourceResolver : PathResourceResolver() {
        override fun getResource(resourcePath: String, location: Resource): Resource? {
            val requested = location.createRelative(resourcePath)
            if (requested.exists() && requested.isReadable) return requested
            if (isApplicationPath(resourcePath)) return null
            return INDEX.takeIf(Resource::exists)
        }
    }

    private companion object {
        const val STATIC_ROOT = "classpath:/static/"

        val INDEX: Resource = ClassPathResource("static/index.html")

        /**
         * Пути, которые обслуживает бэкенд. Оболочку панели вместо них отдавать
         * нельзя: клиент ждёт JSON и молча получил бы разметку.
         */
        val BACKEND_PREFIXES = listOf(
            "api/", "actuator/", "v3/api-docs", "swagger-ui",
        )

        fun isApplicationPath(path: String): Boolean =
            BACKEND_PREFIXES.any(path::startsWith)
    }
}
