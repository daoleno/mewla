// The plugins come through the buildscript classpath: AGP 9 with the Kotlin it bundles.
buildscript {
  repositories { google(); mavenCentral() }
  dependencies {
    classpath("com.android.tools.build:gradle:9.2.1")
    classpath("org.jetbrains.kotlin:kotlin-gradle-plugin:2.2.0")
  }
}
