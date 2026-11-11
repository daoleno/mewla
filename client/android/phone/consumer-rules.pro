# The helper process is started by class name from a shell command, so nothing may strip or rename it.
-keep class com.daoleno.mewla.phone.helper.** { *; }
# The app's device key source is named in this module's manifest (DeviceKeys.load) and made by reflection.
-keep class * implements com.daoleno.mewla.phone.link.DeviceKeys { public <init>(android.content.Context); }
