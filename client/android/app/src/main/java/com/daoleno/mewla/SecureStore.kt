package com.daoleno.mewla

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import org.json.JSONObject
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * The device key and the other secrets, kept exactly where and how the Expo app kept them
 * (expo-secure-store: SharedPreferences "SecureStore", AES-GCM under one Android Keystore key),
 * so the upgrade reads the same device identity and nobody pairs again. Nothing is copied.
 */
class SecureStore(context: Context) {
  private val prefs = context.getSharedPreferences("SecureStore", Context.MODE_PRIVATE)
  private val keyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }

  fun get(key: String): String? {
    val item = (prefs.getString(prefsKey(key), null) ?: prefs.getString(key, null))?.let(::JSONObject) ?: return null
    if (item.optString("scheme") != "aes" || item.optBoolean("requireAuthentication")) return null
    val alias = if (item.optBoolean("usesKeystoreSuffix")) SUFFIXED_ALIAS else BASE_ALIAS
    val secret = (keyStore.getEntry(alias, null) as? KeyStore.SecretKeyEntry)?.secretKey ?: return null
    val cipher = Cipher.getInstance(CIPHER)
    cipher.init(Cipher.DECRYPT_MODE, secret, GCMParameterSpec(item.getInt("tlen"), Base64.decode(item.getString("iv"), Base64.DEFAULT)))
    return String(cipher.doFinal(Base64.decode(item.getString("ct"), Base64.DEFAULT)), Charsets.UTF_8)
  }

  fun set(key: String, value: String) {
    val cipher = Cipher.getInstance(CIPHER)
    cipher.init(Cipher.ENCRYPT_MODE, secretKey())
    val spec = cipher.parameters.getParameterSpec(GCMParameterSpec::class.java)
    val item = JSONObject()
      .put("ct", Base64.encodeToString(cipher.doFinal(value.toByteArray(Charsets.UTF_8)), Base64.NO_WRAP))
      .put("iv", Base64.encodeToString(spec.iv, Base64.NO_WRAP))
      .put("tlen", spec.tLen)
      .put("scheme", "aes")
      .put("usesKeystoreSuffix", true)
      .put("keystoreAlias", SERVICE)
      .put("requireAuthentication", false)
    if (!prefs.edit().putString(prefsKey(key), item.toString()).remove(key).commit()) error("could not save $key")
  }

  fun remove(key: String) {
    prefs.edit().remove(prefsKey(key)).remove(key).commit()
  }

  private fun secretKey(): SecretKey {
    (keyStore.getEntry(SUFFIXED_ALIAS, null) as? KeyStore.SecretKeyEntry)?.let { return it.secretKey }
    val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
    generator.init(
      KeyGenParameterSpec.Builder(SUFFIXED_ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
        .setKeySize(256)
        .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
        .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
        .build(),
    )
    return generator.generateKey()
  }

  private fun prefsKey(key: String) = "$SERVICE-$key"

  companion object {
    private const val SERVICE = "key_v1"
    private const val CIPHER = "AES/GCM/NoPadding"
    private const val BASE_ALIAS = "$CIPHER:$SERVICE"
    private const val SUFFIXED_ALIAS = "$BASE_ALIAS:keystoreUnauthenticated"
  }
}
