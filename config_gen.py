import json
from pathlib import Path


def make_xray_config(srv: dict) -> dict:
    protocol = srv.get("protocol", "").lower()
    transport = srv.get("transport", "tcp").lower()
    host = srv.get("host", "")
    port = srv.get("port", 443)
    cred = srv.get("cred", "")
    params = srv.get("params", {})

    security = (params.get("security") or "tls").lower()

    config = {
        "log": {
            "loglevel": "warning"
        },

        "inbounds": [
            {
                "tag": "socks-in",
                "listen": "127.0.0.1",
                "port": 1080,
                "protocol": "socks",
                "settings": {
                    "auth": "noauth",
                    "udp": True
                },
                "sniffing": {
                    "enabled": True,
                    "destOverride": ["http", "tls", "quic"]
                }
            },
            {
                "tag": "http-in",
                "listen": "127.0.0.1",
                "port": 1081,
                "protocol": "http"
            }
        ],

        "outbounds": [
            {
                "tag": "proxy",
                "protocol": protocol,
                "settings": _get_outbound_settings(protocol, host, port, cred, params),
                "streamSettings": _get_stream_settings(transport, security, host, params)
            },
            {
                "tag": "direct",
                "protocol": "freedom"
            }
        ],

        "routing": {
            "domainStrategy": "AsIs",
            "rules": [
                {
                    "type": "field",
                    "inboundTag": ["socks-in", "http-in"],
                    "outboundTag": "proxy"
                }
            ]
        }
    }

    return config


# ---------------- OUTBOUND ----------------

def _get_outbound_settings(protocol, host, port, cred, params):
    protocol = protocol.lower()

    if protocol == "vless":
        user = {
            "id": cred,
            "encryption": "none"
        }

        if params.get("flow"):
            user["flow"] = params["flow"]

        return {
            "vnext": [
                {
                    "address": host,
                    "port": port,
                    "users": [user]
                }
            ]
        }

    if protocol == "trojan":
        return {
            "servers": [
                {
                    "address": host,
                    "port": port,
                    "password": cred
                }
            ]
        }

    if protocol == "shadowsocks":
        return {
            "servers": [
                {
                    "address": host,
                    "port": port,
                    "method": params.get("method", "aes-256-gcm"),
                    "password": cred
                }
            ]
        }

    if protocol == "vmess":
        return {
            "vnext": [
                {
                    "address": host,
                    "port": port,
                    "users": [
                        {
                            "id": cred,
                            "security": params.get("scy", "auto")
                        }
                    ]
                }
            ]
        }

    return {}


# ---------------- STREAM ----------------

def _get_stream_settings(transport, security, host, params):
    settings = {
        "network": transport
    }

    # ---------------- TLS ----------------
    if security == "tls":
        settings["security"] = "tls"
        tls = {}

        sni = params.get("sni") or host
        if sni:
            tls["serverName"] = sni

        if params.get("fp"):
            tls["fingerprint"] = params["fp"]

        settings["tlsSettings"] = tls

    # ---------------- REALITY ----------------
    elif security == "reality":
        settings["security"] = "reality"

        settings["realitySettings"] = {
            "serverName": params.get("sni") or host,
            "fingerprint": params.get("fp", "chrome"),
            "publicKey": params.get("pbk"),
            "shortId": params.get("sid", ""),
            "spiderX": params.get("spx", ""),
            "show": False
        }

    # ---------------- WS ----------------
    if transport in ("ws", "websocket"):
        settings["network"] = "ws"
        settings["wsSettings"] = {
            "path": params.get("path", "/"),
            "headers": {}
        }

        if params.get("host"):
            settings["wsSettings"]["headers"]["Host"] = params["host"]

    # ---------------- gRPC ----------------
    elif transport == "grpc":
        settings["network"] = "grpc"
        settings["grpcSettings"] = {
            "serviceName": params.get("serviceName", params.get("service", ""))
        }

    return settings


# ---------------- WRITE ----------------

def write_config(config: dict, path: Path):
    path.parent.mkdir(parents=True, exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(config, f, indent=2, ensure_ascii=False)