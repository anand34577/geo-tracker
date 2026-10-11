# MapLibre uses JNI callbacks into these classes.
-keep class org.maplibre.** { *; }
-dontwarn org.maplibre.**

# Tink (EncryptedSharedPreferences) references compile-only Error Prone annotations.
-dontwarn com.google.errorprone.annotations.**
