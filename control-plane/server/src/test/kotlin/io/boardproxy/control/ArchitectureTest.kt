package io.boardproxy.control

import com.tngtech.archunit.core.importer.ImportOption
import com.tngtech.archunit.junit.AnalyzeClasses
import com.tngtech.archunit.junit.ArchTest
import com.tngtech.archunit.lang.ArchRule
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

    @ArchTest
    val sharedDoesNotDependOnFeatures: ArchRule = noClasses()
        .that().resideInAPackage("..shared..")
        .should().dependOnClassesThat().resideInAnyPackage(
            "io.boardproxy.control.access..",
            "io.boardproxy.control.activity..",
            "..provisioning..",
            "..fleet..",
            "..delivery..",
            "..traffic..",
            "..telemetry..",
            "..runtime..",
            "..audit..",
        )
        .allowEmptyShould(true)
}
