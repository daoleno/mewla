plugins { id("com.android.library") }

// Phone use: an agent drives this phone (wireless-ADB self-pairing, a shell helper, an accessibility
// service, a foreground service, notifications, PiP). GitHub APK only: the Play build never links it.
android {
  namespace = "com.daoleno.mewla.phone"
  compileSdk = 36
  defaultConfig { minSdk = 28 }
  testOptions { unitTests.isReturnDefaultValues = true }
}

dependencies {
  implementation("androidx.core:core-ktx:1.17.0")
  testImplementation("junit:junit:4.13.2")
}
