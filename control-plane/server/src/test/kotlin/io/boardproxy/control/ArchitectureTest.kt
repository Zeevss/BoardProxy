package io.boardproxy.control

import com.tngtech.archunit.core.domain.JavaClass
import com.tngtech.archunit.core.importer.ImportOption
import com.tngtech.archunit.junit.AnalyzeClasses
import com.tngtech.archunit.junit.ArchTest
import com.tngtech.archunit.lang.ArchCondition
import com.tngtech.archunit.lang.ArchRule
import com.tngtech.archunit.lang.ConditionEvents
import com.tngtech.archunit.lang.SimpleConditionEvent
import com.tngtech.archunit.lang.syntax.ArchRuleDefinition.classes
import com.tngtech.archunit.lang.syntax.ArchRuleDefinition.noClasses

@AnalyzeClasses(packages = ["io.boardproxy.control"], importOptions = [ImportOption.DoNotIncludeTests::class])
class ArchitectureTest {
    @ArchTest
    val domainIsFrameworkIndependent: ArchRule = noClasses()
        .that().resideInAPackage("..domain..")
        .should().dependOnClassesThat().resideInAnyPackage(
            "org.springframework..",
            "org.jooq..",
            "org.flywaydb..",
            "io.grpc..",
            "bproxy.node.v1..",
        )

    @ArchTest
    val applicationDoesNotKnowDeliveryOrPersistenceFrameworks: ArchRule = noClasses()
        .that().resideInAPackage("..application..")
        .should().dependOnClassesThat().resideInAnyPackage(
            "org.springframework.web..",
            "org.jooq..",
            "org.flywaydb..",
            "io.grpc..",
            "bproxy.node.v1..",
        )

    @ArchTest
    val apiDoesNotReachIntoInfrastructure: ArchRule = noClasses()
        .that().resideInAPackage("..api..")
        .should().dependOnClassesThat().resideInAPackage("..infrastructure..")
        .allowEmptyShould(true)

    /**
     * Правила такого не было — и именно поэтому телеметрия писала DISABLED прямо
     * в каталог, подписки ходили в каталог за keylink'ами, а «пользователь» имел
     * двух владельцев. Связки возникали незамеченными.
     *
     * Контексты общаются только через порты в shared.contracts. Сводит их
     * инфраструктура: она правилом не ограничена, потому что проводка бинов и
     * подписка на события по определению знают обе стороны.
     */
    @ArchTest
    val applicationLayersDoNotCrossContexts: ArchRule = classes()
        .that().resideInAPackage("io.boardproxy.control..application..")
        .should(NotDependOnAnotherContextApplicationLayer)

    @ArchTest
    val sharedDoesNotDependOnFeatures: ArchRule = noClasses()
        .that().resideInAPackage("..shared..")
        .should().dependOnClassesThat().resideInAnyPackage(
            *FEATURE_CONTEXTS.map { "io.boardproxy.control.$it.." }.toTypedArray(),
        )
        .allowEmptyShould(true)
}

private val FEATURE_CONTEXTS = listOf(
    "access", "activity", "audit", "delivery", "fleet",
    "provisioning", "runtime", "subscription", "telemetry",
)

/**
 * Единственная разрешённая пара. Доставка существует ровно для того, чтобы
 * отдавать ноде вывод provisioning, — это направление зависимости и есть её
 * смысл, а не случайная связка.
 *
 * Список намеренно короткий: каждая новая запись здесь должна быть отдельным
 * обсуждённым решением, иначе правило перестанет что-либо значить.
 */
private val ALLOWED_PAIRS: Map<String, Set<String>> = mapOf(
    "delivery" to setOf("provisioning"),
)

/** Ограниченный контекст класса, либо null для shared и корневых классов. */
private fun contextOf(packageName: String): String? = packageName
    .removePrefix("io.boardproxy.control.")
    .substringBefore('.')
    .takeIf { it in FEATURE_CONTEXTS }

private object NotDependOnAnotherContextApplicationLayer :
    ArchCondition<JavaClass>("not depend on the application layer of another bounded context") {

    override fun check(item: JavaClass, events: ConditionEvents) {
        val source = contextOf(item.packageName) ?: return
        item.directDependenciesFromSelf.forEach { dependency ->
            val target = dependency.targetClass
            val targetContext = contextOf(target.packageName) ?: return@forEach
            if (targetContext == source) return@forEach
            if (targetContext in ALLOWED_PAIRS[source].orEmpty()) return@forEach
            if (target.packageName.contains(".application")) {
                events.add(
                    SimpleConditionEvent.violated(
                        item,
                        "${item.name} зависит от ${target.name}: контексты общаются через shared.contracts",
                    ),
                )
            }
        }
    }
}
