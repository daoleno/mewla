package com.daoleno.mewla

import android.view.View
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.core.content.ContextCompat
import com.google.mlkit.vision.barcode.BarcodeScannerOptions
import com.google.mlkit.vision.barcode.BarcodeScanning
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.common.InputImage
import com.longbridge.nsh.NshComponent
import com.longbridge.nsh.NshComponentContext

/**
 * `mewla.qrScanner`: the camera with a QR reader (CameraX + ML Kit). `scan` fires once with
 * the first code read; `unavailable` when the camera is refused or missing.
 */
class QrScanner(private val activity: MainActivity, private val events: NshComponentContext) : NshComponent {
  private val preview = PreviewView(activity)
  private var provider: ProcessCameraProvider? = null
  private var active = false
  private var done = false

  init { instances.add(this) }

  override fun view(): View = preview

  override fun setProp(name: String, value: Any?) {
    if (name != "active") return
    active = value == true
    if (active) start() else stop()
  }

  private fun start() {
    if (!activity.cameraAllowed()) return
    val future = ProcessCameraProvider.getInstance(activity)
    future.addListener({
      val cameras = runCatching { future.get() }.getOrElse { return@addListener events.emit("unavailable", mapOf("reason" to (it.message ?: "no camera"))) }
      if (!active) return@addListener
      provider = cameras
      val shown = Preview.Builder().build().also { it.surfaceProvider = preview.surfaceProvider }
      val reader = BarcodeScanning.getClient(BarcodeScannerOptions.Builder().setBarcodeFormats(Barcode.FORMAT_QR_CODE).build())
      val analysis = ImageAnalysis.Builder().setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST).build()
      analysis.setAnalyzer(ContextCompat.getMainExecutor(activity)) { frame ->
        val image = frame.image
        if (image == null || done) return@setAnalyzer frame.close()
        reader.process(InputImage.fromMediaImage(image, frame.imageInfo.rotationDegrees))
          .addOnSuccessListener { codes ->
            val value = codes.firstOrNull()?.rawValue ?: return@addOnSuccessListener
            if (!done) { done = true; events.emit("scan", mapOf("value" to value)) }
          }
          .addOnCompleteListener { frame.close() }
      }
      runCatching {
        cameras.unbindAll()
        cameras.bindToLifecycle(activity, CameraSelector.DEFAULT_BACK_CAMERA, shown, analysis)
      }.onFailure { events.emit("unavailable", mapOf("reason" to (it.message ?: "no back camera"))) }
    }, ContextCompat.getMainExecutor(activity))
  }

  private fun stop() { provider?.unbindAll() }

  override fun destroy() { stop(); instances.remove(this) }

  companion object {
    private val instances = mutableListOf<QrScanner>()

    fun cameraAnswered(granted: Boolean) {
      for (scanner in instances.filter { it.active }) {
        if (granted) scanner.start() else scanner.events.emit("unavailable", mapOf("reason" to "camera access is off for Mewla"))
      }
    }
  }
}
