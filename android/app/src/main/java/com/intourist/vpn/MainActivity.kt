package com.intourist.vpn

import android.annotation.SuppressLint
import android.app.Activity
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.util.Log
import android.webkit.ConsoleMessage
import android.webkit.JavascriptInterface
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.webkit.WebViewAssetLoader
import org.json.JSONArray
import org.json.JSONObject
import java.util.concurrent.atomic.AtomicBoolean

class MainActivity : AppCompatActivity() {
    private lateinit var webView: WebView
    private lateinit var bridgeApi: VpnBridgeApi
    private var pendingStart: Pair<String, String>? = null
    private val permissionRequestPending = AtomicBoolean(false)

    private val vpnPermission = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        Log.i(TAG, "VpnService.prepare() dialog result: resultCode=${result.resultCode}")
        if (result.resultCode == Activity.RESULT_OK) {
            pendingStart?.let { (mode, config) ->
                Log.i(TAG, "VPN permission granted, launching service (mode=$mode)")
                if (!launchVpnService(mode, config)) {
                    Log.e(TAG, "launchVpnService returned false after permission grant")
                    bridgeApi.onStartRejected("[ERROR] Could not start VPN service.")
                }
            } ?: Log.w(TAG, "VPN permission granted but no pendingStart was recorded")
        } else {
            Log.w(TAG, "VPN permission denied by user")
            bridgeApi.onStartRejected("[ERROR] VPN permission denied.")
        }
        pendingStart = null
        permissionRequestPending.set(false)
    }

    // Android 13+ requires a runtime permission to display notifications, including
    // the foreground-service notification. Requested proactively and independently
    // of the VPN connect flow: startForeground() still succeeds without it, so we
    // never gate connecting on this permission's result.
    private val notificationPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        Log.i(TAG, "POST_NOTIFICATIONS permission granted=$granted")
    }

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Log.i(TAG, "onCreate")

        val isDebuggable = (applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE) != 0
        // Lets you inspect this WebView from chrome://inspect on a connected desktop
        // Chrome. Also required for any of the console logging below to reach a
        // remote DevTools session (Logcat forwarding below works regardless).
        WebView.setWebContentsDebuggingEnabled(isDebuggable)

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(this, android.Manifest.permission.POST_NOTIFICATIONS) !=
            android.content.pm.PackageManager.PERMISSION_GRANTED
        ) {
            notificationPermission.launch(android.Manifest.permission.POST_NOTIFICATIONS)
        }

        bridgeApi = VpnBridgeApi(this)
        bridgeApi.register()

        val assetLoader = WebViewAssetLoader.Builder()
            .addPathHandler("/assets/", WebViewAssetLoader.AssetsPathHandler(this))
            .build()

        webView = WebView(this).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.cacheMode = WebSettings.LOAD_DEFAULT
            addJavascriptInterface(bridgeApi, "AndroidBridge")
            // CRITICAL: without a WebChromeClient override, any console.log/warn/error
            // call and any uncaught JS exception inside the WebView is swallowed
            // completely — nothing reaches Logcat. That produces exactly the "silent
            // failure, zero logs" symptom even when the UI button click never makes
            // it to AndroidBridge at all. This is the single most important piece of
            // visibility into the UI <-> Kotlin handoff.
            webChromeClient = object : WebChromeClient() {
                override fun onConsoleMessage(message: ConsoleMessage): Boolean {
                    val level = when (message.messageLevel()) {
                        ConsoleMessage.MessageLevel.ERROR -> Log.ERROR
                        ConsoleMessage.MessageLevel.WARNING -> Log.WARN
                        ConsoleMessage.MessageLevel.DEBUG -> Log.DEBUG
                        else -> Log.INFO
                    }
                    Log.println(level, "WebViewConsole", "${message.message()} (${message.sourceId()}:${message.lineNumber()})")
                    return true
                }
            }
            webViewClient = object : WebViewClient() {
                override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? {
                    if (request.url.toString() == "qrc:///qtwebchannel/qwebchannel.js") {
                        Log.i(TAG, "Serving QWebChannel shim for ${request.url}")
                        return WebResourceResponse("application/javascript", "UTF-8", QWEBCHANNEL_SHIM.byteInputStream())
                    }
                    return assetLoader.shouldInterceptRequest(request.url)
                }

                override fun onReceivedError(
                    view: WebView,
                    request: WebResourceRequest,
                    error: android.webkit.WebResourceError
                ) {
                    Log.e(TAG, "WebView resource error for ${request.url}: ${error.description}")
                }

                override fun onPageFinished(view: WebView, url: String) {
                    Log.i(TAG, "onPageFinished: $url")
                    bridgeApi.emitInitialState()
                }
            }
        }

        setContentView(webView)
        webView.loadUrl("https://appassets.androidplatform.net/assets/intourist_vps_premium_ui/index.html")
    }

    override fun onDestroy() {
        bridgeApi.unregister()
        webView.destroy()
        super.onDestroy()
    }

    fun startVpn(mode: String, config: String): Boolean {
        Log.i(TAG, "startVpn(mode=$mode) called from bridge")
        return runCatching {
            val prepareIntent = VpnService.prepare(this)
            if (prepareIntent != null) {
                Log.i(TAG, "VpnService.prepare() returned a consent Intent — user has not approved this app yet, launching system dialog")
                if (!permissionRequestPending.compareAndSet(false, true)) {
                    Log.w(TAG, "startVpn ignored: a permission request is already pending")
                    return false
                }
                pendingStart = mode to config
                vpnPermission.launch(prepareIntent)
            } else {
                Log.i(TAG, "VpnService.prepare() returned null — permission already granted, starting service directly")
                return launchVpnService(mode, config)
            }
            true
        }.getOrElse {
            Log.e(TAG, "VpnService.prepare()/launch threw an exception", it)
            permissionRequestPending.set(false)
            false
        }
    }

    private fun launchVpnService(mode: String, config: String): Boolean {
        Log.i(TAG, "launchVpnService(mode=$mode): building intent and calling startForegroundService")
        return runCatching {
            val intent = IntouristVpnService.startIntent(this, mode, config)
            ContextCompat.startForegroundService(this, intent)
            Log.i(TAG, "startForegroundService() dispatched successfully")
        }.onFailure {
            Log.e(TAG, "startForegroundService() threw", it)
        }.isSuccess
    }

    fun emitBridgeSignal(signal: String, value: Any) {
        val payload = when (value) {
            is Boolean -> value.toString()
            is Number -> value.toString()
            is JSONObject, is JSONArray -> value.toString()
            else -> JSONObject.quote(value.toString())
        }
        webView.post {
            webView.evaluateJavascript("window.__intouristEmit && window.__intouristEmit(${JSONObject.quote(signal)}, $payload);", null)
        }
    }

    companion object {
        private const val TAG = "MainActivity"
        private val QWEBCHANNEL_SHIM = """
            (function () {
              function makeSignal(name) {
                var handlers = [];
                return {
                  connect: function (handler) { handlers.push(handler); },
                  emit: function (value) { handlers.slice().forEach(function (h) { try { h(value); } catch (e) {} }); }
                };
              }
              function call(method) {
                return function () {
                  var args = Array.prototype.slice.call(arguments);
                  if (window.AndroidBridge && window.AndroidBridge[method]) {
                    return window.AndroidBridge[method].apply(window.AndroidBridge, args);
                  }
                };
              }
              var bridge = {
                logMessage: makeSignal('logMessage'),
                statusChanged: makeSignal('statusChanged'),
                metricsUpdated: makeSignal('metricsUpdated'),
                serversUpdated: makeSignal('serversUpdated'),
                modeChanged: makeSignal('modeChanged'),
                linkHistoryUpdated: makeSignal('linkHistoryUpdated'),
                connect: call('connect'),
                disconnect: call('disconnect'),
                fetchSubscription: call('fetchSubscription'),
                getPing: call('getPing'),
                selectServer: call('selectServer'),
                connectSelected: call('connectSelected'),
                disconnectVpn: call('disconnectVpn'),
                loadSubscription: call('loadSubscription'),
                pingServers: call('pingServers'),
                openUrl: call('openUrl')
              };
              window.__intouristEmit = function (name, value) {
                if (bridge[name] && bridge[name].emit) bridge[name].emit(value);
              };
              window.qt = window.qt || { webChannelTransport: {} };
              window.QWebChannel = function (_transport, callback) {
                callback({ objects: { bridge: bridge } });
              };
            })();
        """.trimIndent()
    }
}
