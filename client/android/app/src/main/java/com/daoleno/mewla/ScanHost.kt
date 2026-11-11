package com.daoleno.mewla

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.provider.Settings
import androidx.activity.ComponentActivity
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat
import com.google.mlkit.vision.barcode.BarcodeScannerOptions
import com.google.mlkit.vision.barcode.BarcodeScanning
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.common.InputImage
import com.longbridge.nsh.NshHostReply
import com.longbridge.nsh.NshView

/**
 * What pairing needs from the system:
 * - `scan.permission` → `granted` | `undetermined` (the system may still ask) | `denied` (only Settings can change it);
 * - `scan.request` → the same, after the system's camera prompt (or at once when it would not show);
 * - `app.openSettings`: the app's page in the system settings;
 * - `app.share {text}`: the system share sheet;
 * - `scan.image` → `{value}` (the QR code in a picture the person picks; `value` null when it holds
 *   none), or null when they picked nothing.
 * Android never says "asked before"; a preference remembers that the prompt was shown, so a
 * refusal "for good" (no rationale after asking) reads as `denied`, not as never asked.
 */
class ScanHost(private val activity: ComponentActivity, nsh: NshView) {
  private val prefs = activity.getSharedPreferences("mewla.scan", ComponentActivity.MODE_PRIVATE)
  private var asking: NshHostReply? = null
  private val prompt = activity.registerForActivityResult(ActivityResultContracts.RequestPermission()) {
    asking?.resolve(permission())
    asking = null
  }
  private var picking: NshHostReply? = null
  // The system photo picker: no storage permission, only the picture the person chooses.
  private val picker = activity.registerForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
    val reply = picking ?: return@registerForActivityResult
    picking = null
    if (uri == null) return@registerForActivityResult reply.resolve(null)
    val image = runCatching { InputImage.fromFilePath(activity, uri) }.getOrElse {
      return@registerForActivityResult reply.reject("That picture could not be read.")
    }
    val reader = BarcodeScanning.getClient(BarcodeScannerOptions.Builder().setBarcodeFormats(Barcode.FORMAT_QR_CODE).build())
    reader.process(image)
      .addOnSuccessListener { codes -> reply.resolve(mapOf("value" to codes.firstOrNull { !it.rawValue.isNullOrBlank() }?.rawValue)) }
      .addOnFailureListener { reply.reject("The code in that picture could not be read.") }
      .addOnCompleteListener { reader.close() }
  }

  init {
    nsh.handle("scan.permission") { _, reply -> reply.resolve(permission()) }
    nsh.handle("scan.request") { _, reply ->
      if (permission() != "undetermined") return@handle reply.resolve(permission())
      asking?.resolve(permission())
      asking = reply
      prefs.edit().putBoolean(ASKED, true).apply()
      prompt.launch(Manifest.permission.CAMERA)
    }
    nsh.handle("scan.image") { _, reply ->
      picking?.resolve(null)
      picking = reply
      runCatching { picker.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) }
        .onFailure { picking = null; reply.reject(it.message ?: "No picture picker on this phone.") }
    }
    nsh.handle("app.openSettings") { _, reply ->
      runCatching {
        activity.startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", activity.packageName, null)))
      }.fold({ reply.resolve(null) }, { reply.reject(it.message ?: "Settings did not open.") })
    }
    nsh.handle("app.share") { data, reply ->
      val text = (data as? Map<*, *>)?.get("text") as? String ?: return@handle reply.reject("text is required")
      val send = Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, text)
      runCatching { activity.startActivity(Intent.createChooser(send, null)) }
        .fold({ reply.resolve(null) }, { reply.reject(it.message ?: "Nothing to share with.") })
    }
  }

  private fun permission(): String = when {
    ContextCompat.checkSelfPermission(activity, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED -> "granted"
    !prefs.getBoolean(ASKED, false) -> "undetermined"
    activity.shouldShowRequestPermissionRationale(Manifest.permission.CAMERA) -> "undetermined"
    else -> "denied"
  }

  private companion object {
    const val ASKED = "camera_asked"
  }
}
