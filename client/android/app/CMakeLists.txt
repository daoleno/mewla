cmake_minimum_required(VERSION 3.22.1)
project(mewla_terminal)

# libghostty-vt, built from the signed, pinned release by scripts/build-libghostty.sh
# (CI: .github/actions/android-native). MEWLA_GHOSTTY is native/terminal-vt (or -Pmewla.ghostty).
add_library(ghostty_vt SHARED IMPORTED)
set_target_properties(ghostty_vt PROPERTIES IMPORTED_LOCATION "${MEWLA_GHOSTTY}/libs/android/${ANDROID_ABI}/libghostty_vt.so")

# The packer and glyphs both phones share (client/native/terminal), behind a small JNI layer.
set(MEWLA_TERMINAL_NATIVE "${CMAKE_CURRENT_SOURCE_DIR}/../../native/terminal")
add_library(terminal_vt SHARED
  src/main/cpp/jni_terminal.cpp
  "${MEWLA_TERMINAL_NATIVE}/TerminalVt.cpp"
  "${MEWLA_TERMINAL_NATIVE}/TerminalGlyphs.cpp")
target_include_directories(terminal_vt PRIVATE "${MEWLA_GHOSTTY}/include" "${MEWLA_TERMINAL_NATIVE}")
target_link_libraries(terminal_vt ghostty_vt android log)
# 16 KB page-size devices (and Google Play) require 16 KB LOAD alignment.
target_link_options(terminal_vt PRIVATE "-Wl,-z,max-page-size=16384")
