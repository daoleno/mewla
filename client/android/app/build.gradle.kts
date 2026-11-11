import groovy.json.JsonSlurper
import javax.inject.Inject

plugins { id("com.android.application") }

// One version for every client: client/ui/nsh.json. The version code is derived from it, so it
// only ever grows and stays above the Expo app's (141 at 0.3.0) that this one replaces in place.
val uiDir = rootProject.file("../ui")
@Suppress("UNCHECKED_CAST")
val manifest = JsonSlurper().parse(uiDir.resolve("nsh.json")) as Map<String, Any>
val version = manifest["version"] as String
val (major, minor, patch) = version.split(".").map { it.toInt() }
val nshHome = providers.environmentVariable("NSH_HOME").orElse("${System.getProperty("user.home")}/.nsh").get()
val nshCli = providers.environmentVariable("NSH").orElse("$nshHome/bin/nsh").get()
// The release keystore is the one the Expo app is signed with (CI secrets only); without it, debug.
val releaseKeystore = providers.environmentVariable("MEWLA_ANDROID_KEYSTORE").orNull?.takeIf { it.isNotBlank() }
// Where the APK goes: "github" (the sideloaded APK: Phone use, self-update) or "play" (the store build: neither).
// The Expo app chose this with MEWLA_ANDROID_DISTRIBUTION; -Pmewla.channel=play builds the store variant.
val channel = providers.gradleProperty("mewla.channel").orNull ?: "github"
if (channel !in setOf("github", "play")) throw GradleException("mewla.channel must be github or play, not $channel")

// The terminal's libghostty-vt: built once per ABI by scripts/build-libghostty.sh into app/modules/terminal-vt
// (it moves into client/ at the cutover). -Pmewla.ghostty=<dir> points elsewhere.
val ghostty = file(providers.gradleProperty("mewla.ghostty").orNull ?: "${rootDir}/../../app/modules/terminal-vt")
val abis = (providers.gradleProperty("mewla.abis").orNull ?: "arm64-v8a,x86_64").split(",")
for (abi in abis) {
  val so = ghostty.resolve("libs/android/$abi/libghostty_vt.so")
  if (!so.exists()) throw GradleException("libghostty-vt for $abi is missing ($so). Build it: ./scripts/build-libghostty.sh --abis $abi, or pass -Pmewla.abis / -Pmewla.ghostty.")
}

android {
  namespace = "com.daoleno.mewla"
  compileSdk = 36
  defaultConfig {
    applicationId = "com.daoleno.mewla"
    minSdk = 28
    targetSdk = 36
    versionCode = major * 1_000_000 + minor * 10_000 + patch * 100
    versionName = version
    // -Pmewla.abis=arm64-v8a for a phone-only build (half the size).
    ndk { abiFilters += abis }
    externalNativeBuild { cmake { arguments += "-DMEWLA_GHOSTTY=${ghostty.absolutePath}" } }
    buildConfigField("String", "CHANNEL", "\"$channel\"")
  }
  flavorDimensions += "identity"
  productFlavors {
    create("production") { dimension = "identity"; resValue("string", "app_name", "Mewla") }
    // Installs beside the store app: the preview builds of the NSH client.
    create("preview") { dimension = "identity"; applicationIdSuffix = ".preview"; resValue("string", "app_name", "Mewla Preview") }
  }
  signingConfigs {
    if (releaseKeystore != null) create("release") {
      storeFile = file(releaseKeystore)
      storePassword = System.getenv("MEWLA_ANDROID_KEYSTORE_PASSWORD")
      keyAlias = System.getenv("MEWLA_ANDROID_KEY_ALIAS")
      keyPassword = System.getenv("MEWLA_ANDROID_KEY_PASSWORD")
    }
  }
  buildTypes {
    getByName("debug") { applicationIdSuffix = ".debug" }
    getByName("release") {
      signingConfig = signingConfigs.findByName("release") ?: signingConfigs.getByName("debug")
      isMinifyEnabled = true
      isShrinkResources = true
      proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
    }
  }
  buildFeatures { buildConfig = true; resValues = true }
  packaging { jniLibs { useLegacyPackaging = true } }
  testOptions { unitTests.isReturnDefaultValues = true }
  externalNativeBuild { cmake { path = file("CMakeLists.txt"); version = "3.22.1" } }
  ndkVersion = "27.1.12297006"
  sourceSets {
    getByName("main") {
      jniLibs.directories.add(ghostty.resolve("libs/android").path)
      // ChannelHosts: what this channel adds to the app (src/github or src/play).
      java.directories.add("src/$channel/java")
      kotlin.directories.add("src/$channel/java")
    }
  }
}

// The app itself: client/ui (and the core it imports) bundled and compiled to bytecode, as assets/nsh.
abstract class BundleUi : DefaultTask() {
  @get:InputDirectory @get:PathSensitive(PathSensitivity.RELATIVE) abstract val ui: DirectoryProperty
  @get:InputDirectory @get:PathSensitive(PathSensitivity.RELATIVE) abstract val core: DirectoryProperty
  @get:Input abstract val nsh: Property<String>
  @get:OutputDirectory abstract val assets: DirectoryProperty
  @get:Inject abstract val exec: ExecOperations

  @TaskAction fun bundle() {
    val out = assets.get().asFile.resolve("nsh")
    out.deleteRecursively()
    exec.exec { commandLine(nsh.get(), "build", "--release", "-o", out.path, ui.get().asFile.path) }
    // The source map is for `nsh symbolicate`, not for the phone.
    out.resolve("bundle.js.map").let { it.copyTo(assets.get().asFile.resolveSibling("bundleUi.js.map"), overwrite = true); it.delete() }
  }
}
val bundleUi = tasks.register<BundleUi>("bundleUi") {
  ui.set(uiDir)
  core.set(rootProject.file("../core"))
  nsh.set(nshCli)
}
// Push: Firebase reads its app entry from resources (what the google-services plugin writes). The plugin
// fails every variant whose package has no entry in the file, so the entry is looked up here instead and a
// variant without one (the preview identity, until Firebase lists it) builds without push.
val googleServices = rootProject.file("../../app/google-services.json")
@Suppress("UNCHECKED_CAST")
fun firebaseEntry(packageName: String): Map<String, String>? {
  if (!googleServices.isFile) return null
  val json = JsonSlurper().parse(googleServices) as Map<String, Any>
  val project = json["project_info"] as Map<String, Any>
  val client = (json["client"] as List<Map<String, Any>>).firstOrNull {
    ((it["client_info"] as Map<String, Any>)["android_client_info"] as Map<String, Any>)["package_name"] == packageName
  } ?: return null
  val apiKey = (client["api_key"] as List<Map<String, Any>>).first()["current_key"] as String
  return mapOf(
    "google_app_id" to ((client["client_info"] as Map<String, Any>)["mobilesdk_app_id"] as String),
    "gcm_defaultSenderId" to (project["project_number"] as String),
    "google_api_key" to apiKey,
    "google_crash_reporting_api_key" to apiKey,
    "project_id" to (project["project_id"] as String),
    "google_storage_bucket" to ((project["storage_bucket"] as? String) ?: ""),
  )
}

androidComponents {
  onVariants { variant ->
    variant.sources.assets?.addGeneratedSourceDirectory(bundleUi, BundleUi::assets)
    firebaseEntry(variant.applicationId.get())?.forEach { (name, value) ->
      variant.resValues.put(variant.makeResValueKey("string", name), com.android.build.api.variant.ResValue(value, null))
    }
  }
}

dependencies {
  debugImplementation(files("$nshHome/embed/android/nsh-dev.aar"))
  releaseImplementation(files("$nshHome/embed/android/nsh.aar"))
  implementation("androidx.activity:activity:1.8.2")
  implementation("androidx.core:core-ktx:1.17.0")
  implementation("androidx.camera:camera-camera2:1.6.0")
  implementation("androidx.camera:camera-lifecycle:1.6.0")
  implementation("androidx.camera:camera-view:1.6.0")
  implementation("com.google.mlkit:barcode-scanning:17.3.0")
  implementation("com.google.firebase:firebase-messaging:25.0.1")
  implementation("androidx.lifecycle:lifecycle-common:2.9.4")
  if (channel == "github") {
    implementation(project(":phone"))
    implementation(project(":appupdate"))
  }
  testImplementation("junit:junit:4.13.2")
  testImplementation("org.json:json:20240303")
}
