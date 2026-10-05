"""Patch the WeatherFlow UDP weewx driver for running in a container.

The driver is downloaded at a pinned commit by the Dockerfile, and these are
exact-text edits to that file. Each edit must match exactly once, so a changed
upstream file fails the image build instead of shipping half-patched.

1. Parse datagrams as JSON, not with eval(). Upstream turns each datagram into
   Python source and evaluates it, so anyone who can send a UDP packet to the
   station's port on the local network can run code inside weewx.
2. Log through Python's logging module, which weewx 5 configures, instead of
   syslog(3). A container has no /dev/log, so syslog calls vanish silently.

Usage: python3 patch-driver.py weatherflowudp.py
"""

import sys

EDITS = [
    (
        "m1=eval(m0)\n                except SyntaxError:",
        "m1=json.loads(m[0])\n                except ValueError:",
    ),
    (
        "def logmsg(level, msg):\n"
        "    syslog.syslog(level, 'weatherflowudp: %s: %s' %\n"
        "                  (threading.currentThread().getName(), msg))\n",
        "import logging\n"
        "_log = logging.getLogger('user.weatherflowudp')\n"
        "_LEVELS = {syslog.LOG_DEBUG: logging.DEBUG, syslog.LOG_INFO: logging.INFO, syslog.LOG_ERR: logging.ERROR}\n"
        "\n"
        "def logmsg(level, msg):\n"
        "    _log.log(_LEVELS.get(level, logging.INFO), '%s: %s', threading.current_thread().name, msg)\n",
    ),
]


def main(path):
    with open(path, encoding="utf-8") as f:
        src = f.read()
    for old, new in EDITS:
        n = src.count(old)
        if n != 1:
            sys.exit(f"{path}: expected exactly one match for {old.splitlines()[0]!r}, found {n}")
        src = src.replace(old, new)
    if "eval(" in src:
        sys.exit(f"{path}: eval( is still present after patching")
    with open(path, "w", encoding="utf-8") as f:
        f.write(src)
    print(f"patched {path}")


if __name__ == "__main__":
    main(sys.argv[1])
