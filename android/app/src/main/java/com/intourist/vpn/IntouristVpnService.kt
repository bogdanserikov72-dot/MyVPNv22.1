package com.intourist.vpn

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.IpPrefix
import android.net.VpnService
import android.os.Build
import android.os.IBinder
import android.os.ParcelFileDescriptor
import android.system.OsConstants
import android.util.Log
import androidx.core.app.ServiceCompat
import com.intourist.gomobile.intouristcore.Intouristcore
import com.intourist.gomobile.intouristcore.LogSink
import com.intourist.gomobile.intouristcore.SocketProtector
import org.json.JSONArray
import org.json.JSONObject
import java.net.Inet4Address
import java.net.Inet6Address
import java.net.InetAddress
import java.net.URI
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

class IntouristVpnService : VpnService(), LogSink, SocketProtector {
    private var tun: ParcelFileDescriptor? = null
    private var detachedTunFd: Long = -1
    private val coreExecutor = Executors.newSingleThreadExecutor { runnable ->
        Thread(runnable, "IntouristGoCore").apply { isDaemon = true }
    }

    override fun onBind(intent: Intent?): IBinder? = super.onBind(intent)

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent == null) return START_NOT_STICKY

        when (intent.action) {
            ACTION_STOP -> {
                stopVpn()
                return START_NOT_STICKY
            }
            ACTION_START -> startVpn(intent)
        }

        return START_STICKY
    }

    override fun onDestroy() {
        stopVpn()
        coreExecutor.shutdownNow()
        super.onDestroy()
    }

    private fun startVpn(intent: Intent) {
        Log.i(TAG, "startVpn: onStartCommand received ACTION_START")
        if (serviceActive.get() || runCatching { Intouristcore.isStarting() || Intouristcore.isRunning() }.getOrDefault(false)) {
            Log.i(TAG, "Ignoring duplicate VPN start request")
            broadcastLog("[INFO] VPN start request ignored: already starting or running.")
            return
        }
        serviceActive.set(true)
        stopping.set(false)

        // This block used to run unguarded: if buildNotification()/startForeground()
        // threw (e.g. a missing/renamed notification channel, or a foreground-service
        // launch restriction on newer Android versions), the exception propagated out
        // of onStartCommand and crashed the process with no application-level log line
        // of our own — easy to miss and easy to mistake for "nothing happened".
        try {
            val notification = buildNotification("Connecting")
            // ServiceCompat.startForeground picks the right underlying
            // Service#startForeground overload for the running API level: it passes
            // the foreground-service type on Q+ (required, and enforced, on API 34/U)
            // and safely ignores that argument on older versions.
            ServiceCompat.startForeground(
                this,
                NOTIFICATION_ID,
                notification,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE
            )
            Log.i(TAG, "startForeground() succeeded (notificationId=$NOTIFICATION_ID)")
        } catch (t: Throwable) {
            Log.e(TAG, "startForeground() failed — service cannot continue", t)
            broadcastLog("[ERROR] Could not start foreground service: ${t.message}")
            serviceActive.set(false)
            stopSelf()
            return
        }

        val mode = intent.getStringExtra(EXTRA_MODE) ?: MODE_HELPER
        val config = intent.getStringExtra(EXTRA_CONFIG) ?: ""
        Log.i(TAG, "Dispatching startVpnRuntime(mode=$mode) on core executor thread")

        coreExecutor.execute {
            startVpnRuntime(mode, config)
        }
    }

    private fun startVpnRuntime(mode: String, config: String) {
        try {
            val fd = establishVpn(config).detachFd().toLong()
            tun = null
            detachedTunFd = fd

            // Sanity-check the descriptor before handing it to the Go core. A
            // negative/zero fd here means Builder.establish()/detachFd() handed us
            // something unusable — calling into startHelperMode/startSubMode with it
            // would either throw deep inside the Go core's syscalls or, worse, block
            // forever on a channel waiting for I/O that can never happen, which is
            // indistinguishable from "nothing is happening" at this layer.
            //
            // NOTE: mobile/intouristcore/main.go was not included in the project
            // archive this fix was generated from (only the compiled
            // gomobile-intouristcore.aar/.jar are present), so the equivalent guard
            // could not be added on the Go side directly. If you can share that file,
            // add a mirrored check right after AttachTunFd/the fd parameter is
            // received: verify fd > 0 (e.g. via unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
            // returning no error) and return a descriptive error immediately instead
            // of proceeding — that closes the gap on the Go side too.
            if (fd <= 0) {
                Log.e(TAG, "establishVpn produced an invalid tunFd=$fd — aborting before calling into Go core")
                broadcastLog("[ERROR] Invalid TUN descriptor (fd=$fd); VPN cannot start.")
                serviceActive.set(false)
                stopVpn()
                return
            }

            Log.i(TAG, "Starting mode=$mode with tunFd=$fd")
            broadcastLog("[INFO] TUN established (fd=$fd), handing off to core (mode=$mode)")

            when (mode) {
                MODE_SUB -> Intouristcore.startSubMode(config, fd, this, this)
                else -> Intouristcore.startHelperMode(config, fd, this, this)
            }
            Log.i(TAG, "Intouristcore.start${if (mode == MODE_SUB) "Sub" else "Helper"}Mode() call returned without throwing")
        } catch (t: Throwable) {
            Log.e(TAG, "Failed to start VPN", t)
            broadcastLog("[ERROR] ${t.message ?: "Failed to start VPN"}")
            serviceActive.set(false)
            stopVpn()
        }
    }

    private fun establishVpn(config: String): ParcelFileDescriptor {
        tun?.close()

        val builder = Builder()
            .setSession("Intourist VPN")
            .setMtu(1500)
            .addAddress("10.0.0.2", 32)
            .addDnsServer("1.1.1.1")
            .addRoute("0.0.0.0", 0)
            .allowFamily(OsConstants.AF_INET)
            .setBlocking(false)

        excludeSelfFromVpn(builder)
        excludeResolvedUpstreams(builder, config)

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            builder.setMetered(false)
        }

        return requireNotNull(builder.establish()) { "VpnService.Builder.establish returned null" }
            .also { tun = it }
    }

    private fun excludeSelfFromVpn(builder: Builder) {
        runCatching {
            builder.addDisallowedApplication(packageName)
            Log.i(TAG, "Excluded $packageName from VPN to prevent core routing loops")
        }.onFailure {
            Log.w(TAG, "Could not exclude $packageName from VPN", it)
        }
    }

    private fun excludeResolvedUpstreams(builder: Builder, config: String) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return

        extractUpstreamHosts(config).forEach { host ->
            runCatching {
                InetAddress.getAllByName(host).forEach { address ->
                    val prefixLength = when (address) {
                        is Inet4Address -> 32
                        is Inet6Address -> 128
                        else -> return@forEach
                    }
                    builder.excludeRoute(IpPrefix(address, prefixLength))
                    Log.i(TAG, "Excluded upstream $host/${address.hostAddress} from VPN route")
                }
            }.onFailure {
                Log.w(TAG, "Could not resolve/exclude upstream host $host", it)
            }
        }
    }

    private fun stopVpn() {
        if (!stopping.compareAndSet(false, true)) return

        Log.i(TAG, "Stopping VPN")
        runCatching { Intouristcore.stop() }
        runCatching { tun?.close() }
        tun = null
        detachedTunFd = -1
        serviceActive.set(false)
        broadcastState(false, "")
        stopForegroundCompat()
        stopSelf()
    }

    override fun protect(fd: Long): Boolean {
        val ok = protect(fd.toInt())
        Log.d(TAG, "protect($fd)=$ok")
        return ok
    }

    override fun onLog(message: String) {
        Log.i(TAG, message)
        broadcastLog(message)
    }

    override fun onError(message: String) {
        Log.e(TAG, message)
        broadcastLog(message)
    }

    override fun onStateChanged(connected: Boolean, mode: String) {
        if (!connected) {
            serviceActive.set(false)
        }
        broadcastState(connected, mode)
    }

    private fun extractUpstreamHosts(config: String): Set<String> {
        val hosts = linkedSetOf<String>()
        if (config.isBlank()) return hosts

        runCatching {
            val json = JSONObject(config)
            collectJsonHosts(json, hosts)
        }

        Regex("""(?im)^\s*url\s*:\s*['"]?([^'"\s]+)""")
            .findAll(config)
            .mapNotNull { match -> runCatching { URI(match.groupValues[1]).host }.getOrNull() }
            .filterTo(hosts) { it.isNotBlank() }

        Regex("""(?i)\b(?:address|server|host)\b\s*[:=]\s*['"]?([A-Za-z0-9_.:-]+)""")
            .findAll(config)
            .map { it.groupValues[1].trim('[', ']', '"', '\'') }
            .filter { it.isNotBlank() && !it.equals("127.0.0.1") && !it.equals("localhost", ignoreCase = true) }
            .filterTo(hosts) { !it.contains(":") || runCatching { InetAddress.getByName(it) }.isSuccess }

        return hosts
    }

    private fun collectJsonHosts(value: Any?, hosts: MutableSet<String>) {
        when (value) {
            is JSONObject -> {
                value.keys().forEach { key ->
                    val child = value.opt(key)
                    if (key in HOST_KEYS && child is String && child.isNotBlank()) {
                        hosts.add(child)
                    }
                    collectJsonHosts(child, hosts)
                }
            }
            is JSONArray -> {
                for (i in 0 until value.length()) {
                    collectJsonHosts(value.opt(i), hosts)
                }
            }
        }
    }

    private fun broadcastLog(message: String) {
        sendBroadcast(Intent(ACTION_LOG).setPackage(packageName).putExtra(EXTRA_MESSAGE, message))
    }

    private fun broadcastState(connected: Boolean, mode: String) {
        sendBroadcast(
            Intent(ACTION_STATE)
                .setPackage(packageName)
                .putExtra(EXTRA_CONNECTED, connected)
                .putExtra(EXTRA_MODE, mode)
        )
    }

    private fun buildNotification(state: String): Notification {
        createNotificationChannel()
        val stopIntent = Intent(this, IntouristVpnService::class.java).setAction(ACTION_STOP)
        val stopPendingIntent = PendingIntent.getService(
            this,
            1,
            stopIntent,
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )

        val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Notification.Builder(this, CHANNEL_ID)
        } else {
            @Suppress("DEPRECATION")
            Notification.Builder(this)
        }

        return builder
            .setSmallIcon(android.R.drawable.stat_sys_download_done)
            .setContentTitle("Intourist VPN")
            .setContentText(state)
            .setOngoing(true)
            .addAction(android.R.drawable.ic_menu_close_clear_cancel, "Disconnect", stopPendingIntent)
            .build()
    }

    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val manager = getSystemService(NotificationManager::class.java)
        val channel = NotificationChannel(CHANNEL_ID, "VPN connection", NotificationManager.IMPORTANCE_LOW)
        manager.createNotificationChannel(channel)
    }

    private fun stopForegroundCompat() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
            stopForeground(STOP_FOREGROUND_REMOVE)
        } else {
            @Suppress("DEPRECATION")
            stopForeground(true)
        }
    }

    companion object {
        private const val TAG = "IntouristVpnService"
        private const val CHANNEL_ID = "intourist_vpn"
        private const val NOTIFICATION_ID = 1001

        const val ACTION_START = "com.intourist.vpn.action.START"
        const val ACTION_STOP = "com.intourist.vpn.action.STOP"
        const val ACTION_LOG = "com.intourist.vpn.action.LOG"
        const val ACTION_STATE = "com.intourist.vpn.action.STATE"

        const val EXTRA_MODE = "mode"
        const val EXTRA_CONFIG = "config"
        const val EXTRA_MESSAGE = "message"
        const val EXTRA_CONNECTED = "connected"

        const val MODE_HELPER = "helper"
        const val MODE_SUB = "sub"

        private val HOST_KEYS = setOf("address", "server", "serverName", "host", "sni")
        private val serviceActive = AtomicBoolean(false)
        private val stopping = AtomicBoolean(false)

        fun startIntent(context: Context, mode: String, config: String): Intent =
            Intent(context, IntouristVpnService::class.java)
                .setAction(ACTION_START)
                .putExtra(EXTRA_MODE, mode)
                .putExtra(EXTRA_CONFIG, config)

        fun stopIntent(context: Context): Intent =
            Intent(context, IntouristVpnService::class.java).setAction(ACTION_STOP)
    }
}
