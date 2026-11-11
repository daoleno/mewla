package com.daoleno.mewla

import android.Manifest
import android.content.pm.PackageManager
import android.util.Log
import android.view.View
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.core.content.ContextCompat
import com.google.mlkit.vision.barcode.BarcodeScanner
import com.google.mlkit.vision.barcode.BarcodeScannerOptions
import com.google.mlkit.vision.barcode.BarcodeScanning
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.common.InputImage
import com.longbridge.nsh.NshComponent
import com.longbridge.nsh.NshComponentContext

/**
 * `mewla.qrScanner`: the camera with a QR reader (CameraX + ML Kit), running while `active`.
 * `scan {value}` for each code read (a value again only after 1.5 s, so a rejected code still
 * in view is not re-sent every frame); `unavailable {reason}` with `permission` (the app asks,
 * through `scan.request`), `camera` (no camera, or it would not start) or `reader`. Nothing here
 * may take the app down: every failure is an event.
 */
class QrScanner(private val activity: MainActivity, private val events: NshComponentContext) : NshComponent {
  private val preview = PreviewView(activity)
  private var provider: ProcessCameraProvider? = null
  private var reader: BarcodeScanner? = null
  private var active = false
  private var last = ""
  private var lastAt = 0L

  override fun view(): View = preview

  override fun setProp(name: String, value: Any?) {
    if (name != "active") return
    active = value == true
    if (active) start() else stop()
  }

  private fun start() {
    if (ContextCompat.checkSelfPermission(activity, Manifest.permission.CAMERA) != PackageManager.PERMISSION_GRANTED) {
      return unavailable("permission", "camera access is off for Mewla")
    }
    val future = runCatching { ProcessCameraProvider.getInstance(activity) }.getOrElse { return unavailable("camera", it) }
    future.addListener({
      if (!active) return@addListener
      runCatching { bind(future.get()) }.onFailure { unavailable("camera", it) }
    }, ContextCompat.getMainExecutor(activity))
  }

  private fun bind(cameras: ProcessCameraProvider) {
    // The reader is ML Kit's component; R8 must keep its registrar (proguard-rules.pro, verifyRegistrars).
    val reader = reader ?: runCatching {
      BarcodeScanning.getClient(BarcodeScannerOptions.Builder().setBarcodeFormats(Barcode.FORMAT_QR_CODE).build())
    }.getOrElse { return unavailable("reader", it) }.also { reader = it }
    provider = cameras
    val shown = Preview.Builder().build().also { it.surfaceProvider = preview.surfaceProvider }
    val analysis = ImageAnalysis.Builder().setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST).build()
    analysis.setAnalyzer(ContextCompat.getMainExecutor(activity)) { frame ->
      val image = frame.image
      if (image == null || !active) return@setAnalyzer frame.close()
      val read = runCatching { reader.process(InputImage.fromMediaImage(image, frame.imageInfo.rotationDegrees)) }
        .getOrElse { frame.close(); return@setAnalyzer unavailable("reader", it) }
      read.addOnSuccessListener { codes -> codes.firstOrNull()?.rawValue?.let(::found) }
        .addOnCompleteListener { frame.close() }
    }
    cameras.unbindAll()
    cameras.bindToLifecycle(activity, CameraSelector.DEFAULT_BACK_CAMERA, shown, analysis)
  }

  private fun found(value: String) {
    val now = System.currentTimeMillis()
    if (!active || (value == last && now - lastAt < REPEAT_MS)) return
    last = value
    lastAt = now
    events.emit("scan", mapOf("value" to value))
  }

  private fun unavailable(reason: String, error: Any) {
    if (error is Throwable) Log.w(TAG, "scanner unavailable: $reason", error)
    events.emit("unavailable", mapOf("reason" to reason, "message" to ((error as? Throwable)?.message ?: error.toString())))
  }

  private fun stop() {
    runCatching { provider?.unbindAll() }
  }

  override fun destroy() {
    active = false
    stop()
    runCatching { reader?.close() }
    reader = null
  }

  private companion object {
    const val TAG = "Mewla"
    const val REPEAT_MS = 1500L
  }
}
