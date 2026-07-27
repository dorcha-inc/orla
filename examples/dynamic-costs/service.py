"""A toy cost service that Orla polls for a backend's current price.

The service answers every GET with the cost contract Orla expects:

    {"input_cost_per_mtoken": 0.1, "output_cost_per_mtoken": 0.4}

The price alternates between an off-peak and a peak level so a
watcher can see Orla's recorded costs move. The first half of each
period serves the base price and the second half serves the base
price times the peak multiplier, like time-of-use electricity
pricing.

    uv run service.py

Environment: PORT (default 9090), INPUT_COST and OUTPUT_COST (base
dollars per million tokens, defaults 0.10 and 0.40), PERIOD (seconds
per full off-peak and peak cycle, default 120), PEAK_MULTIPLIER
(default 4.0).
"""

from __future__ import annotations

import json
import os
import time
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = int(os.environ.get("PORT", "9090"))
INPUT_COST = float(os.environ.get("INPUT_COST", "0.10"))
OUTPUT_COST = float(os.environ.get("OUTPUT_COST", "0.40"))
PERIOD = float(os.environ.get("PERIOD", "120"))
PEAK_MULTIPLIER = float(os.environ.get("PEAK_MULTIPLIER", "4.0"))


def current_price() -> dict[str, float]:
    peak = (time.time() % PERIOD) >= PERIOD / 2
    factor = PEAK_MULTIPLIER if peak else 1.0
    return {
        "input_cost_per_mtoken": round(INPUT_COST * factor, 6),
        "output_cost_per_mtoken": round(OUTPUT_COST * factor, 6),
    }


class PriceHandler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:
        body = json.dumps(current_price()).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format: str, *args: object) -> None:
        print(f"{self.address_string()} {format % args}  ->  {current_price()}")


def main() -> None:
    server = HTTPServer(("127.0.0.1", PORT), PriceHandler)
    print(f"cost service listening on http://127.0.0.1:{PORT}")
    print(
        f"off-peak {INPUT_COST}/{OUTPUT_COST} per Mtoken, peak x{PEAK_MULTIPLIER}, period {PERIOD}s"
    )
    server.serve_forever()


if __name__ == "__main__":
    main()
