package com.intourist.vpn

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.util.Base64
import android.util.Log
import android.webkit.JavascriptInterface
import androidx.core.content.ContextCompat
import org.json.JSONArray
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.InetSocketAddress
import java.net.Socket
import java.net.URL
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

class VpnBridgeApi(
    private val activity: MainActivity
) {
    private val main = Handler(Looper.getMainLooper())
    private val io = Executors.newCachedThreadPool()
    private val servers = mutableListOf(helperServer())
    private var selectedIndex = 0
    private val linkHistory = ArrayDeque<String>()
    private val startInFlight = AtomicBoolean(false)
    private val connected = AtomicBoolean(false)
    private val lastConnectAtMs = AtomicLong(0L)

    private val vpnReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            when (intent.action) {
                IntouristVpnService.ACTION_LOG -> emit("logMessage", intent.getStringExtra(IntouristVpnService.EXTRA_MESSAGE) ?: "")
                IntouristVpnService.ACTION_STATE -> {
                    val isConnected = intent.getBooleanExtra(IntouristVpnService.EXTRA_CONNECTED, false)
                    connected.set(isConnected)
                    startInFlight.set(false)
                    emit("statusChanged", isConnected)
                    if (!isConnected) emit("modeChanged", "")
                }
            }
        }
    }

    fun register() {
        val filter = IntentFilter().apply {
            addAction(IntouristVpnService.ACTION_LOG)
            addAction(IntouristVpnService.ACTION_STATE)
        }
        ContextCompat.registerReceiver(activity, vpnReceiver, filter, ContextCompat.RECEIVER_NOT_EXPORTED)
    }

    fun unregister() {
        runCatching { activity.unregisterReceiver(vpnReceiver) }
    }

    fun onStartRejected(message: String) {
        startInFlight.set(false)
        connected.set(false)
        emit("statusChanged", false)
        emit("modeChanged", "")
        emit("logMessage", message)
    }

    @JavascriptInterface
    fun connect(mode: String, serverDataJSON: String) {
        // This Log call is intentionally the very first thing that happens, and it
        // goes straight to Logcat rather than through emit()/evaluateJavascript.
        // If this line never shows up in `adb logcat`, the click never reached the
        // JavascriptInterface at all — the break is in the WebView/JS layer, not
        // here. If it does show up, the break is further down this pipeline.
        Log.i(TAG, "connect() called from JS bridge: mode=$mode")
        val now = System.currentTimeMillis()
        if (now - lastConnectAtMs.get() < CONNECT_DEBOUNCE_MS) {
            Log.i(TAG, "connect() ignored: debounce window active")
            emit("logMessage", "[INFO] Connect ignored: debounce window is active.")
            return
        }
        lastConnectAtMs.set(now)

        if (connected.get() || !startInFlight.compareAndSet(false, true)) {
            Log.i(TAG, "connect() ignored: already connected/starting (connected=${connected.get()})")
            emit("logMessage", "[INFO] Connect ignored: VPN is already starting or connected.")
            return
        }

        val normalizedMode = if (mode.equals("sub", ignoreCase = true)) IntouristVpnService.MODE_SUB else IntouristVpnService.MODE_HELPER
        val config = runCatching {
            if (normalizedMode == IntouristVpnService.MODE_SUB) makeXrayConfig(serverDataJSON) else serverDataJSON
        }.getOrElse {
            Log.e(TAG, "connect() failed to prepare config", it)
            startInFlight.set(false)
            emit("logMessage", "[ERROR] Could not prepare VPN config: ${it.message}")
            return
        }

        emit("modeChanged", if (normalizedMode == IntouristVpnService.MODE_SUB) "Mode: subscription" else "Mode: helper")
        emit("logMessage", "[INFO] Connect requested ($normalizedMode)")
        Log.i(TAG, "connect(): posting startVpn(mode=$normalizedMode) to main thread")

        main.post {
            runCatching {
                if (activity.startVpn(normalizedMode, config)) {
                    Log.i(TAG, "connect(): activity.startVpn returned true")
                    emit("logMessage", "[INFO] Starting Intourist VPN ($normalizedMode)")
                } else {
                    Log.w(TAG, "connect(): activity.startVpn returned false")
                    startInFlight.set(false)
                    emit("logMessage", "[INFO] Connect ignored: VPN permission/start is already pending.")
                }
            }.onFailure {
                Log.e(TAG, "connect(): activity.startVpn threw", it)
                startInFlight.set(false)
                emit("logMessage", "[ERROR] Failed to start VPN: ${it.message}")
            }
        }
    }

    @JavascriptInterface
    fun disconnect() {
        Log.i(TAG, "disconnect() called from JS bridge")
        startInFlight.set(false)
        connected.set(false)
        main.post { activity.stopService(IntouristVpnService.stopIntent(activity)) }
        emit("statusChanged", false)
        emit("modeChanged", "")
        emit("logMessage", "[INFO] Disconnected.")
    }

    @JavascriptInterface
    fun fetchSubscription(url: String) {
        loadSubscription(url)
    }

    @JavascriptInterface
    fun getPing() {
        pingServers()
    }

    @JavascriptInterface
    fun selectServer(index: Int) {
        if (index in servers.indices) selectedIndex = index
    }

    @JavascriptInterface
    fun connectSelected() {
        Log.i(TAG, "connectSelected() called from JS bridge (selectedIndex=$selectedIndex)")
        emit("logMessage", "[INFO] Connect button received by Android bridge.")
        val server = servers.getOrNull(selectedIndex) ?: servers.first()
        val mode = if (server.optString("kind") == "helper") IntouristVpnService.MODE_HELPER else IntouristVpnService.MODE_SUB
        connect(mode, server.toString())
    }

    @JavascriptInterface
    fun disconnectVpn() {
        disconnect()
    }

    @JavascriptInterface
    fun loadSubscription(text: String) {
        val value = text.trim()
        if (value.isEmpty()) return

        if (VPN_URI_PREFIXES.any { value.startsWith(it) }) {
            parseServerUri(value)?.let {
                servers.clear()
                servers.add(helperServer())
                servers.add(it)
                selectedIndex = 1
                rememberLink(value)
                emitServers()
                emit("logMessage", "[INFO] Server added from URI.")
            } ?: emit("logMessage", "[ERROR] Could not parse URI.")
            return
        }

        emit("logMessage", "[INFO] Loading subscription...")
        io.execute {
            runCatching {
                val body = URL(value).openConnection().let { connection ->
                    connection as HttpURLConnection
                    connection.connectTimeout = 15_000
                    connection.readTimeout = 20_000
                    connection.inputStream.bufferedReader().use { it.readText() }
                }
                parseSubscription(body)
            }.onSuccess { parsed ->
                main.post {
                    if (parsed.isEmpty()) {
                        emit("logMessage", "[WARN] Subscription is empty or unsupported.")
                    } else {
                        servers.clear()
                        servers.add(helperServer())
                        servers.addAll(parsed)
                        selectedIndex = 1
                        rememberLink(value)
                        emitServers()
                        emit("logMessage", "[INFO] Loaded servers: ${parsed.size}")
                        pingServers()
                    }
                }
            }.onFailure {
                main.post { emit("logMessage", "[ERROR] Subscription load failed: ${it.message}") }
            }
        }
    }

    @JavascriptInterface
    fun pingServers() {
        servers.forEachIndexed { index, server ->
            if (server.optString("kind") == "helper") return@forEachIndexed
            io.execute {
                val ms = ping(server.optString("host"), server.optInt("port", 443))
                main.post {
                    server.put("ping", ms)
                    emitServers()
                }
            }
        }
    }

    @JavascriptInterface
    fun openUrl(url: String) {
        main.post {
            runCatching {
                activity.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
            }
        }
    }

    fun emitInitialState() {
        emitServers()
        emit("statusChanged", false)
        emit("linkHistoryUpdated", JSONArray(linkHistory.toList()).toString())
    }

    private fun emitServers() {
        emit("serversUpdated", JSONArray(servers).toString())
    }

    private fun emit(signal: String, value: Any) {
        main.post { activity.emitBridgeSignal(signal, value) }
    }

    private fun rememberLink(link: String) {
        linkHistory.remove(link)
        linkHistory.addFirst(link)
        while (linkHistory.size > 10) linkHistory.removeLast()
        emit("linkHistoryUpdated", JSONArray(linkHistory.toList()).toString())
    }

    private fun parseSubscription(body: String): List<JSONObject> {
        val decoded = runCatching {
            decodeBase64(body.trim())
        }.getOrDefault(body)

        return decoded.lineSequence()
            .flatMap { it.split("\n").asSequence() }
            .map { it.trim() }
            .filter { line -> VPN_URI_PREFIXES.any { line.startsWith(it) } }
            .mapNotNull { parseServerUri(it) }
            .toList()
    }

    private fun parseServerUri(uri: String): JSONObject? = runCatching {
        when {
            uri.startsWith("vmess://") -> parseVmess(uri)
            uri.startsWith("vless://") -> parseGeneric(uri, "vless")
            uri.startsWith("trojan://") -> parseGeneric(uri, "trojan")
            uri.startsWith("ss://") -> parseGeneric(uri, "shadowsocks")
            else -> null
        }
    }.getOrNull()

    private fun parseVmess(uri: String): JSONObject {
        val raw = uri.removePrefix("vmess://")
        val decoded = decodeBase64(raw)
        val vmess = JSONObject(decoded)
        return JSONObject()
            .put("name", vmess.optString("ps", "VMess"))
            .put("host", vmess.getString("add"))
            .put("port", vmess.optInt("port", 443))
            .put("protocol", "vmess")
            .put("transport", vmess.optString("net", "tcp"))
            .put("cred", vmess.optString("id"))
            .put("kind", "sub")
            .put("params", JSONObject().apply {
                put("security", vmess.optString("tls", "tls").ifBlank { "tls" })
                put("sni", vmess.optString("sni", vmess.optString("host", "")))
                put("path", vmess.optString("path", "/"))
                put("host", vmess.optString("host", ""))
                put("scy", vmess.optString("scy", "auto"))
            })
    }

    private fun parseGeneric(uri: String, protocol: String): JSONObject {
        val parsed = Uri.parse(uri)
        val params = JSONObject()
        for (key in parsed.queryParameterNames) {
            params.put(key, parsed.getQueryParameter(key))
        }
        val userInfo = parsed.encodedUserInfo.orEmpty()
        val credential = if (protocol == "shadowsocks" && "@" !in userInfo) {
            decodeBase64(userInfo).substringAfter(":")
        } else {
            Uri.decode(userInfo).substringAfterLast(":")
        }

        return JSONObject()
            .put("name", parsed.fragment ?: protocol.uppercase())
            .put("host", parsed.host ?: "")
            .put("port", parsed.port.takeIf { it > 0 } ?: 443)
            .put("protocol", protocol)
            .put("transport", parsed.getQueryParameter("type") ?: "tcp")
            .put("cred", credential)
            .put("kind", "sub")
            .put("params", params)
    }

    private fun makeXrayConfig(serverDataJSON: String): String {
        val server = JSONObject(serverDataJSON)
        val protocol = server.optString("protocol").lowercase()
        val host = server.optString("host")
        val port = server.optInt("port", 443)
        val cred = server.optString("cred")
        val params = server.optJSONObject("params") ?: JSONObject()

        return JSONObject()
            .put("log", JSONObject().put("loglevel", "warning"))
            .put("inbounds", JSONArray()
                .put(JSONObject()
                    .put("tag", "socks-in")
                    .put("listen", "127.0.0.1")
                    .put("port", 1080)
                    .put("protocol", "socks")
                    .put("settings", JSONObject().put("auth", "noauth").put("udp", true))
                    .put("sniffing", JSONObject().put("enabled", true).put("destOverride", JSONArray(listOf("http", "tls", "quic")))))
                .put(JSONObject()
                    .put("tag", "http-in")
                    .put("listen", "127.0.0.1")
                    .put("port", 1081)
                    .put("protocol", "http")))
            .put("outbounds", JSONArray()
                .put(JSONObject()
                    .put("tag", "proxy")
                    .put("protocol", protocol)
                    .put("settings", outboundSettings(protocol, host, port, cred, params))
                    .put("streamSettings", streamSettings(server.optString("transport", "tcp"), params, host)))
                .put(JSONObject().put("tag", "direct").put("protocol", "freedom")))
            .put("routing", JSONObject()
                .put("domainStrategy", "AsIs")
                .put("rules", JSONArray().put(JSONObject()
                    .put("type", "field")
                    .put("inboundTag", JSONArray(listOf("socks-in", "http-in")))
                    .put("outboundTag", "proxy"))))
            .toString()
    }

    private fun outboundSettings(protocol: String, host: String, port: Int, cred: String, params: JSONObject): JSONObject =
        when (protocol) {
            "vless" -> JSONObject().put("vnext", JSONArray().put(JSONObject()
                .put("address", host).put("port", port)
                .put("users", JSONArray().put(JSONObject().put("id", cred).put("encryption", "none").apply {
                    params.optString("flow").takeIf { it.isNotBlank() }?.let { put("flow", it) }
                }))))
            "vmess" -> JSONObject().put("vnext", JSONArray().put(JSONObject()
                .put("address", host).put("port", port)
                .put("users", JSONArray().put(JSONObject().put("id", cred).put("security", params.optString("scy", "auto"))))))
            "trojan" -> JSONObject().put("servers", JSONArray().put(JSONObject().put("address", host).put("port", port).put("password", cred)))
            "shadowsocks" -> JSONObject().put("servers", JSONArray().put(JSONObject()
                .put("address", host).put("port", port)
                .put("method", params.optString("method", "aes-256-gcm"))
                .put("password", cred)))
            else -> JSONObject()
        }

    private fun streamSettings(transport: String, params: JSONObject, host: String): JSONObject {
        val security = params.optString("security", "tls")
        val settings = JSONObject().put("network", transport)
        if (security == "tls") {
            settings.put("security", "tls")
            settings.put("tlsSettings", JSONObject().put("serverName", params.optString("sni", host)))
        } else if (security == "reality") {
            settings.put("security", "reality")
            settings.put("realitySettings", JSONObject()
                .put("serverName", params.optString("sni", host))
                .put("fingerprint", params.optString("fp", "chrome"))
                .put("publicKey", params.optString("pbk"))
                .put("shortId", params.optString("sid", "")))
        }
        if (transport == "ws" || transport == "websocket") {
            settings.put("network", "ws")
            settings.put("wsSettings", JSONObject()
                .put("path", params.optString("path", "/"))
                .put("headers", JSONObject().apply {
                    params.optString("host").takeIf { it.isNotBlank() }?.let { put("Host", it) }
                }))
        } else if (transport == "grpc") {
            settings.put("grpcSettings", JSONObject().put("serviceName", params.optString("serviceName", params.optString("service", ""))))
        }
        return settings
    }

    private fun ping(host: String, port: Int): Int {
        if (host.isBlank()) return -1
        val started = System.nanoTime()
        return runCatching {
            Socket().use { it.connect(InetSocketAddress(host, port), 2500) }
            ((System.nanoTime() - started) / 1_000_000L).toInt()
        }.getOrDefault(-1)
    }

    private fun helperServer(): JSONObject = JSONObject()
        .put("name", "Intourist Helper")
        .put("host", "bridge")
        .put("port", 443)
        .put("protocol", "helper")
        .put("kind", "helper")
        .put("ping", JSONObject.NULL)

    companion object {
        private const val TAG = "VpnBridgeApi"
        private val VPN_URI_PREFIXES = listOf("vless://", "vmess://", "ss://", "trojan://")
        private const val CONNECT_DEBOUNCE_MS = 1500L

        private fun decodeBase64(value: String): String {
            val normalized = value
                .replace('-', '+')
                .replace('_', '/')
                .let { it + "=".repeat((4 - it.length % 4) % 4) }
            return String(Base64.decode(normalized, Base64.DEFAULT))
        }
    }
}
