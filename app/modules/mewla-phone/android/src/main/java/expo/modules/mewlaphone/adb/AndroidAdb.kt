// Adapted from Mika (https://github.com/wangzan101/mika), Apache License 2.0. See NOTICE.
package expo.modules.mewlaphone.adb

import android.content.Context
import android.net.ssl.SSLSockets
import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import expo.modules.mewlaphone.adb.AdbIdentity
import expo.modules.mewlaphone.adb.AdbKeyStore
import expo.modules.mewlaphone.adb.AdbTls
import expo.modules.mewlaphone.adb.KeyingMaterialExporter
import expo.modules.mewlaphone.adb.WrappedKeyFile
import java.io.File
import java.security.KeyStore
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.net.ssl.SSLException

object AndroidAdb {
    private const val KEYSTORE = "AndroidKeyStore"
    private const val WRAP_ALIAS = "mewla_adb_key_wrap"

    val exporter = KeyingMaterialExporter { socket, label, length ->
        SSLSockets.exportKeyingMaterial(socket, label, ByteArray(0), length) ?: throw SSLException("keying material export failed")
    }

    fun keyStore(context: Context): AdbKeyStore =
        WrappedKeyFile(File(context.applicationContext.noBackupFilesDir, "adb/adbkey.bin")) { wrappingKey() }

    fun identity(context: Context): AdbIdentity = AdbIdentity.loadOrCreate(keyStore(context), "mewla@${Build.MODEL}")

    fun tls(identity: AdbIdentity) = AdbTls(identity, exporter)

    fun forget(context: Context) {
        keyStore(context).clear()
        runCatching { KeyStore.getInstance(KEYSTORE).apply { load(null) }.deleteEntry(WRAP_ALIAS) }
    }

    private fun wrappingKey(): SecretKey {
        val ks = KeyStore.getInstance(KEYSTORE).apply { load(null) }
        (ks.getKey(WRAP_ALIAS, null) as? SecretKey)?.let { return it }
        val gen = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE)
        gen.init(
            KeyGenParameterSpec.Builder(WRAP_ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return gen.generateKey()
    }
}
