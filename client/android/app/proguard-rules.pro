# NSH reaches its Java side and the components it hosts through JNI and reflection.
-keep class com.longbridge.nsh.** { *; }
-keep class com.daoleno.mewla.** { *; }
-dontwarn com.longbridge.nsh.**

# Firebase and ML Kit find their components by the class names in the manifest and build them with the
# no-arg constructor, by reflection. firebase-components 16 (what ML Kit alone brings) keeps the classes
# but not that constructor, and R8's full mode then drops it: ML Kit's barcode component went missing
# and the first "Scan code" in a release build crashed (11-11). The build checks the result: verifyRegistrars.
-keep class * implements com.google.firebase.components.ComponentRegistrar { void <init>(); }
