// The plugins come through the buildscript classpath: AGP 9 with the Kotlin it bundles.
buildscript {
  repositories { google(); maven { url = uri("https://maven-central.storage-download.googleapis.com/maven2") }; mavenCentral() }
  dependencies {
    classpath("com.android.tools.build:gradle:9.2.1")
    classpath("org.jetbrains.kotlin:kotlin-gradle-plugin:2.2.0")
  }
}
