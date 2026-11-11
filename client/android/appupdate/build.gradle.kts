plugins { id("com.android.library") }

// The in-app update of the GitHub APK: download the release APK, check its size and SHA-256, ask for
// "install unknown apps" when needed, and hand it to the system installer. GitHub APK only.
android {
  namespace = "com.daoleno.mewla.appupdate"
  compileSdk = 36
  defaultConfig { minSdk = 28 }
  testOptions { unitTests.isReturnDefaultValues = true }
}

dependencies {
  implementation("androidx.core:core-ktx:1.17.0")
  testImplementation("junit:junit:4.13.2")
}
