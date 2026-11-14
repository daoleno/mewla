// The terminal's native half for Android (Vt.kt): libghostty-vt through the packer both phones
// share (client/native/terminal/TerminalVt.cpp) and the glyphs the terminal draws itself
// (TerminalGlyphs.cpp). One handle per terminal view.
#include <jni.h>

#include <string>
#include <vector>

#include "TerminalGlyphs.h"
#include "TerminalVt.h"

namespace {

MewlaVt* vtOf(jlong handle) { return reinterpret_cast<MewlaVt*>(handle); }

// UTF-8 to a Java string through UTF-16 (NewStringUTF's modified UTF-8 breaks emoji).
jstring javaString(JNIEnv* env, const uint8_t* data, size_t length) {
    std::u16string out;
    out.reserve(length);
    for (size_t i = 0; i < length;) {
        uint32_t c = data[i];
        int extra = c < 0x80 ? 0 : (c >> 5) == 0x6 ? 1 : (c >> 4) == 0xE ? 2 : (c >> 3) == 0x1E ? 3 : -1;
        if (extra < 0 || i + size_t(extra) >= length) { out.push_back(0xFFFD); i++; continue; }
        c &= extra == 0 ? 0x7F : extra == 1 ? 0x1F : extra == 2 ? 0x0F : 0x07;
        for (int k = 1; k <= extra; k++) c = (c << 6) | (data[i + k] & 0x3F);
        i += extra + 1;
        if (c >= 0x10000) {
            c -= 0x10000;
            out.push_back(char16_t(0xD800 + (c >> 10)));
            out.push_back(char16_t(0xDC00 + (c & 0x3FF)));
        } else {
            out.push_back(char16_t(c));
        }
    }
    return env->NewString(reinterpret_cast<const jchar*>(out.data()), jsize(out.size()));
}

}  // namespace

extern "C" {

JNIEXPORT jlong JNICALL Java_com_daoleno_mewla_terminal_Vt_nativeCreateTerminal(JNIEnv*, jobject, jint cols, jint rows) {
    if (cols <= 0 || rows <= 0 || cols > 0xFFFF || rows > 0xFFFF) return 0;
    return reinterpret_cast<jlong>(mewla_vt_create(uint16_t(cols), uint16_t(rows)));
}

JNIEXPORT void JNICALL Java_com_daoleno_mewla_terminal_Vt_nativeDestroyTerminal(JNIEnv*, jobject, jlong handle) {
    mewla_vt_destroy(vtOf(handle));
}

JNIEXPORT void JNICALL Java_com_daoleno_mewla_terminal_Vt_nativeResize(JNIEnv*, jobject, jlong handle, jint cols, jint rows, jfloat cellWidth, jfloat cellHeight) {
    if (!handle || cols <= 0 || rows <= 0) return;
    mewla_vt_resize(vtOf(handle), uint16_t(cols), uint16_t(rows), cellWidth, cellHeight);
}

JNIEXPORT jboolean JNICALL Java_com_daoleno_mewla_terminal_Vt_nativeSetTheme(JNIEnv* env, jobject, jlong handle, jint foreground, jint background, jint cursor, jintArray palette) {
    if (!handle || !palette || env->GetArrayLength(palette) < 256) return JNI_FALSE;
    std::vector<uint32_t> colors(256);
    env->GetIntArrayRegion(palette, 0, 256, reinterpret_cast<jint*>(colors.data()));
    for (auto& c : colors) c &= 0xFFFFFF;
    return mewla_vt_set_theme(vtOf(handle), uint32_t(foreground) & 0xFFFFFF, uint32_t(background) & 0xFFFFFF,
        uint32_t(cursor) & 0xFFFFFF, colors.data(), colors.size()) ? JNI_TRUE : JNI_FALSE;
}

JNIEXPORT void JNICALL Java_com_daoleno_mewla_terminal_Vt_nativeWriteBytes(JNIEnv* env, jobject, jlong handle, jbyteArray data) {
    if (!handle || !data) return;
    jsize length = env->GetArrayLength(data);
    jbyte* bytes = env->GetByteArrayElements(data, nullptr);
    mewla_vt_write(vtOf(handle), reinterpret_cast<const uint8_t*>(bytes), size_t(length));
    env->ReleaseByteArrayElements(data, bytes, JNI_ABORT);
}

// [meta IntArray, text CharArray]: TerminalVt.h's frame.
JNIEXPORT jobjectArray JNICALL Java_com_daoleno_mewla_terminal_Vt_nativeFrame(JNIEnv* env, jobject, jlong handle, jboolean forceFull) {
    if (!handle) return nullptr;
    MewlaVtFrame frame{};
    if (!mewla_vt_frame(vtOf(handle), forceFull == JNI_TRUE, &frame)) return nullptr;
    jintArray meta = env->NewIntArray(jsize(frame.meta_count));
    env->SetIntArrayRegion(meta, 0, jsize(frame.meta_count), frame.meta);
    jcharArray text = env->NewCharArray(jsize(frame.text_count));
    if (frame.text_count) env->SetCharArrayRegion(text, 0, jsize(frame.text_count), reinterpret_cast<const jchar*>(frame.text));
    jobjectArray out = env->NewObjectArray(2, env->FindClass("java/lang/Object"), nullptr);
    env->SetObjectArrayElement(out, 0, meta);
    env->SetObjectArrayElement(out, 1, text);
    env->DeleteLocalRef(meta);
    env->DeleteLocalRef(text);
    return out;
}

JNIEXPORT jstring JNICALL Java_com_daoleno_mewla_terminal_Vt_nativeTakeClipboard(JNIEnv* env, jobject, jlong handle) {
    if (!handle) return nullptr;
    const uint8_t* data = nullptr;
    size_t length = 0;
    if (!mewla_vt_take_clipboard(vtOf(handle), &data, &length)) return nullptr;
    return javaString(env, data, length);
}

// Pasted text as the bytes for the program (TerminalVt.h mewla_vt_paste); null when nothing.
JNIEXPORT jbyteArray JNICALL Java_com_daoleno_mewla_terminal_Vt_nativePaste(JNIEnv* env, jobject, jlong handle, jbyteArray data) {
    if (!handle || !data) return nullptr;
    jsize length = env->GetArrayLength(data);
    std::vector<uint8_t> in(static_cast<size_t>(length));
    env->GetByteArrayRegion(data, 0, length, reinterpret_cast<jbyte*>(in.data()));
    const uint8_t* out = nullptr;
    size_t outLength = 0;
    if (!mewla_vt_paste(vtOf(handle), in.data(), in.size(), &out, &outLength)) return nullptr;
    jbyteArray result = env->NewByteArray(jsize(outLength));
    env->SetByteArrayRegion(result, 0, jsize(outLength), reinterpret_cast<const jbyte*>(out));
    return result;
}

// [kind, points, alpha, stroke, p0..p7] per step (12 floats); empty when the font draws it.
JNIEXPORT jfloatArray JNICALL Java_com_daoleno_mewla_terminal_Vt_nativeGlyphOps(JNIEnv* env, jobject, jint codepoint, jint width, jint height) {
    MewlaGlyphOp ops[64];
    int32_t n = mewla_glyph_ops(uint32_t(codepoint), width, height, ops, 64);
    std::vector<float> flat;
    flat.reserve(size_t(n) * 12);
    for (int32_t i = 0; i < n; i++) {
        flat.push_back(float(ops[i].kind));
        flat.push_back(float(ops[i].points));
        flat.push_back(ops[i].alpha);
        flat.push_back(ops[i].stroke);
        for (float v : ops[i].p) flat.push_back(v);
    }
    jfloatArray out = env->NewFloatArray(jsize(flat.size()));
    if (!flat.empty()) env->SetFloatArrayRegion(out, 0, jsize(flat.size()), flat.data());
    return out;
}

JNIEXPORT jint JNICALL Java_com_daoleno_mewla_terminal_Vt_nativeGlyphThickness(JNIEnv*, jobject, jint height) {
    return mewla_glyph_thickness(height);
}

}  // extern "C"
