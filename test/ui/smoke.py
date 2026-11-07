"""Exercise the live read-only UI through an installed Chromium browser's DevTools API."""

import argparse
import base64
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time
import urllib.request

import websocket


class Browser:
    def __init__(self, connection):
        self.connection = connection
        self.sequence = 0
        self.errors = []

    def call(self, method, params=None):
        self.sequence += 1
        request_id = self.sequence
        self.connection.send(json.dumps({"id": request_id, "method": method, "params": params or {}}))
        while True:
            response = json.loads(self.connection.recv())
            if response.get("method") == "Runtime.exceptionThrown":
                self.errors.append(response["params"])
            if response.get("id") == request_id:
                if "error" in response:
                    raise AssertionError(response["error"])
                return response.get("result", {})

    def evaluate(self, expression):
        result = self.call("Runtime.evaluate", {"expression": expression, "returnByValue": True})
        if "exceptionDetails" in result:
            raise AssertionError(result["exceptionDetails"])
        return result.get("result", {}).get("value")

    def wait(self, expression, timeout=20):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if self.evaluate(expression):
                return
            time.sleep(0.1)
        raise AssertionError(f"Timed out: {expression}")

    def screenshot(self, path):
        result = self.call("Page.captureScreenshot", {"format": "png", "captureBeyondViewport": True})
        path.write_bytes(base64.b64decode(result["data"]))


def check(browser, args):
    browser.call("Runtime.enable")
    browser.call("Network.enable")
    browser.call("Emulation.setDeviceMetricsOverride", {"width": 1440, "height": 1150, "deviceScaleFactor": 1, "mobile": False})
    browser.call("Page.navigate", {"url": args.url})
    browser.wait("document.querySelectorAll('.device-card').length === 8")
    assert browser.evaluate("document.querySelectorAll('.route-step').length === 6")
    if args.expect_fresh:
        browser.wait("document.querySelector('#fresh-count').textContent.trim().startsWith('8')")
        assert browser.evaluate("document.querySelector('#process-state').textContent === 'Готов'")
    browser.screenshot(args.output / "overview-desktop.png")

    browser.evaluate("document.querySelector('#search').value='IN6';document.querySelector('#search').dispatchEvent(new Event('input'))")
    assert browser.evaluate("document.querySelectorAll('.device-card').length === 1")
    browser.evaluate("document.querySelector('[data-device=\"reflow-oven-01\"]').click()")
    assert browser.evaluate("document.querySelector('#detail').open")
    assert browser.evaluate("document.querySelector('#detail-title').textContent.includes('IN6')")
    assert browser.evaluate("document.querySelector('#detail-body a').href.includes('var-equipment=reflow-oven-01')")
    browser.screenshot(args.output / "equipment-detail.png")
    browser.call("Input.dispatchKeyEvent", {"type": "keyDown", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27})
    browser.call("Input.dispatchKeyEvent", {"type": "keyUp", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27})
    browser.wait("!document.querySelector('#detail').open")
    browser.evaluate("document.querySelector('#reset').click();document.querySelectorAll('.route-step')[5].click()")
    assert browser.evaluate("document.querySelectorAll('.device-card').length === 3")
    browser.evaluate("document.querySelector('#reset').click()")
    browser.call("Emulation.setDeviceMetricsOverride", {"width": 390, "height": 844, "deviceScaleFactor": 1, "mobile": True})
    assert browser.evaluate("document.documentElement.scrollWidth <= window.innerWidth")
    browser.screenshot(args.output / "overview-mobile.png")

    # Block the browser's read request only; do not mutate the running workshop.
    browser.call("Network.setBlockedURLs", {"urls": ["*/api/v1/overview*"]})
    browser.wait("!document.querySelector('#refresh').disabled")
    browser.evaluate("document.querySelector('#refresh').click()")
    browser.wait("document.querySelector('#connection').classList.contains('error')")
    assert browser.evaluate("document.querySelector('#process-state').textContent === 'Неизвестно'")
    assert browser.evaluate("document.querySelectorAll('.device-card .badge.good').length === 0")
    assert browser.evaluate("document.querySelectorAll('.device-card').length === 8")
    browser.call("Network.setBlockedURLs", {"urls": []})
    browser.evaluate("document.querySelector('#refresh').click()")
    browser.wait("!document.querySelector('#connection').classList.contains('error')")
    assert not browser.errors, browser.errors


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--browser", required=True, help="Path to an installed Chromium/Chrome/Brave binary")
    parser.add_argument("--url", default="http://127.0.0.1:8081")
    parser.add_argument("--output", type=Path, default=Path(".cache/ui-browser"))
    parser.add_argument("--expect-fresh", action="store_true")
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="factory-ui-browser-") as profile:
        with (args.output / "browser.log").open("w") as log:
            process = subprocess.Popen([args.browser, "--headless", "--disable-gpu", "--disable-dev-shm-usage",
                                        "--no-first-run", "--no-default-browser-check", "--disable-background-networking",
                                        "--remote-debugging-port=0", f"--user-data-dir={profile}", "about:blank"],
                                       stdout=log, stderr=log, start_new_session=True)
            connection = None
            try:
                port_file = Path(profile) / "DevToolsActivePort"
                deadline = time.monotonic() + 15
                while not port_file.exists():
                    if process.poll() is not None or time.monotonic() > deadline:
                        raise RuntimeError(f"Browser did not start; see {args.output / 'browser.log'}")
                    time.sleep(0.1)
                port = port_file.read_text().splitlines()[0]
                with urllib.request.urlopen(f"http://127.0.0.1:{port}/json/list", timeout=5) as response:
                    target = next(item for item in json.load(response) if item["type"] == "page")
                connection = websocket.create_connection(target["webSocketDebuggerUrl"], timeout=25, suppress_origin=True)
                check(Browser(connection), args)
            finally:
                if connection:
                    try:
                        connection.send(json.dumps({"id": 999999, "method": "Browser.close"}))
                    except websocket.WebSocketException:
                        pass
                    connection.close()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait()
                finally:
                    # Reap this browser's remaining helpers before removing its profile.
                    try:
                        os.killpg(process.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
    print("PASS: live inventory, search, workstation filter, device dialog, Escape, Grafana links, mobile layout, API outage and recovery")


if __name__ == "__main__":
    main()
