// Google's Maven Central mirror before mavenCentral(): repo.maven.apache.org answered CI with 429 (11-24); an
// artifact the mirror lacks falls through to mavenCentral().
dependencyResolutionManagement {
  repositories { google(); maven { url = uri("https://maven-central.storage-download.googleapis.com/maven2") }; mavenCentral() }
}
rootProject.name = "mewla"
include(":app")
// GitHub-channel only (the app depends on them when -Pmewla.channel=github, the default): Phone use and the
// in-app update. Plain Android libraries; the app's src/github/ChannelHosts.kt puts them behind `nsh.handle`.
include(":phone", ":appupdate")
