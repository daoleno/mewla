dependencyResolutionManagement { repositories { google(); mavenCentral() } }
rootProject.name = "mewla"
include(":app")
// GitHub-channel only (the app depends on them when -Pmewla.channel=github, the default): Phone use and the
// in-app update. Plain Android libraries; the app's src/github/ChannelHosts.kt puts them behind `nsh.handle`.
include(":phone", ":appupdate")
